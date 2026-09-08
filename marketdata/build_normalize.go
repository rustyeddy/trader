package marketdata

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/alpaca"
	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
	"github.com/rustyeddy/trader/marketdata/internal/provider/stooq"
)

// normalizeAndPublish executes one ActionNormalizeCanonical entry: reads
// the raw partition for (instrument, interval, year, month), normalizes
// it (normalizeOANDASequence, #76), and publishes the resulting
// (Manifest, BarSet) through the canonical store.
//
// # Abort, never partial-publish, on a genuine data problem
//
// A recordOutcomeSuspicious or recordOutcomeRejected record — a crossed
// market, an impossible Bar shape, a duplicate/out-of-order/misaligned
// timestamp — aborts the entire partition: nothing is published, and
// the returned error names every such record. This is deliberately
// different from an ordinary coverage gap: these outcomes mean the
// input itself is wrong, not merely that data does not exist yet, and
// silently excluding the bad record and publishing the rest would be
// exactly the "silently publish a complete result from incomplete
// inputs" issue #81's acceptance criteria rule out. A caller who wants
// the good records anyway must fix the raw data (or authorize an
// explicit repair, see ActionRepairRaw) and rebuild.
//
// A recordOutcomeIncomplete record (OANDA's own complete flag was
// false) is different in kind: it is excluded from the published
// BarSet — canonical Bar carries no completeness flag at all (#79's own
// finding), so an incomplete record simply is not persisted — but does
// not abort the build. The resulting absence is exactly what Coverage
// (#79) already reports as a gap; that is the correct, honest
// representation, not a build failure.
//
// Manifest.Span always covers the requested calendar month in full
// (monthStart to the next month's start), independent of how many bars
// actually ended up present — a BarSet's Span is a coverage claim, not
// a promise of bar density (#79's own precedent).
func (m *Manager) normalizeAndPublish(ctx context.Context, action Action) (PublishResult, error) {
	// RawRoot is checked here, not in Build, because normalizeAndPublish
	// is the only canonical build path that actually reads raw data —
	// see Build's own doc comment for why a W1-only (or otherwise
	// raw-independent) Plan must not fail on this before it even
	// executes.
	if m.rawRoot == "" {
		return PublishResult{}, fmt.Errorf("marketdata: build: %w: raw root is not configured", ErrInvalidConfig)
	}
	rawInterval, ok := intervalToRawInterval(action.Interval)
	if !ok {
		return PublishResult{}, fmt.Errorf("interval %s has no raw partition to normalize from", action.Interval)
	}
	symbol, err := m.resolveRawSymbol(action.Instrument)
	if err != nil {
		return PublishResult{}, err
	}

	normalized, fingerprint, basis, adjustmentPolicy, feed, calendarVersion, err := m.readAndNormalizeRaw(ctx, rawInterval, symbol, action)
	if err != nil {
		return PublishResult{}, err
	}

	if badErr := firstBadOutcomeError(normalized); badErr != nil {
		return PublishResult{}, badErr
	}

	var bars []Bar
	for _, nr := range normalized {
		if nr.outcome == recordOutcomeAccepted {
			bars = append(bars, nr.bar)
		}
	}

	monthStart := time.Date(action.Year, action.Month, 1, 0, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(monthStart, monthStart.AddDate(0, 1, 0))
	if err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: %w", err)
	}
	bs := BarSet{Instrument: action.Instrument, Interval: action.Interval, Span: span, Basis: basis, Bars: bars}
	if err := bs.Validate(); err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: assembled bar set: %w", err)
	}

	manifest := Manifest{
		Provider:         m.providerName,
		Instrument:       action.Instrument,
		Interval:         action.Interval,
		Span:             span,
		Basis:            basis,
		AdjustmentPolicy: adjustmentPolicy,
		Feed:             feed,
		SchemaVersion:    canonicalSchemaVersion,
		RawFingerprint:   fingerprint,
		BuilderVersion:   builderVersion,
		ValidatorVersion: validatorVersion,
		ResamplerVersion: noResampler,
		CalendarVersion:  calendarVersion,
		BuiltAt:          m.clock.Now(),
		BarCount:         len(bars),
	}
	if len(bars) > 0 {
		manifest.FirstBar = bars[0].Time
		manifest.LastBar = bars[len(bars)-1].Time
	}
	if err := manifest.Validate(); err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: assembled manifest: %w", err)
	}
	if err := manifest.Matches(bs); err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: %w", err)
	}

	if err := m.publishCanonicalMonth(ctx, symbol, action.Interval, action.Year, action.Month, manifest, bs); err != nil {
		return PublishResult{}, fmt.Errorf("publish: %w", err)
	}

	return PublishResult{Action: action, Manifest: manifest, BarCount: len(bars)}, nil
}

// firstBadOutcomeError reports a single, itemized error naming every
// recordOutcomeSuspicious/recordOutcomeRejected entry in normalized, or
// nil if there are none. Every bad record is named, not just the first,
// so a caller diagnosing a failed build sees the full extent of the
// problem in one pass rather than fixing and rebuilding once per record.
func firstBadOutcomeError(normalized []normalizedRecord) error {
	var bad []normalizedRecord
	for _, nr := range normalized {
		if nr.outcome == recordOutcomeSuspicious || nr.outcome == recordOutcomeRejected {
			bad = append(bad, nr)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	err := fmt.Errorf("marketdata: build: %d record(s) failed normalization, aborting (no partial publish): %s at %s: %v",
		len(bad), bad[0].outcome, bad[0].time, bad[0].err)
	for _, nr := range bad[1:] {
		err = fmt.Errorf("%w; %s at %s: %v", err, nr.outcome, nr.time, nr.err)
	}
	return err
}

// calendarVersionUSEquityV1 is the CalendarVersion a Stooq-sourced
// build records now that USEquityCalendar (issue #296, EQ-03) performs
// real D1 boundary-alignment validation against m.calendar — the
// equity-side analogue of calendarVersionCurrent, naming the specific
// Calendar implementation/version a dataset was actually validated
// against, in place of the honest-but-negative "stooq-unaligned-v1"
// this constant replaces.
const calendarVersionUSEquityV1 = "usequitycalendar-v1"

// readAndNormalizeRaw reads the raw partition for (rawInterval, symbol,
// action.Year, action.Month) and normalizes it, dispatched to the
// concrete provider implementation named by m.providerName (ADR-047's
// internal provider seam). It returns the normalized records, the raw
// partition's content fingerprint, and the PriceBasis/AdjustmentPolicy/
// CalendarVersion the resulting canonical dataset should record —
// oanda's own bid-basis, FXCalendar-aligned build is entirely unchanged
// from before this seam existed; stooq is the second, natively-written
// implementation.
func (m *Manager) readAndNormalizeRaw(ctx context.Context, rawInterval, symbol string, action Action) ([]normalizedRecord, string, PriceBasis, AdjustmentPolicy, string, string, error) {
	switch m.providerName {
	case "stooq":
		if rawInterval != string(stooq.RawD1) {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", fmt.Errorf("marketdata: stooq: only %s is supported, got %s", D1, action.Interval)
		}
		// The recorded CalendarVersion below (calendarVersionUSEquityV1)
		// is a specific claim about which Calendar implementation
		// actually validated this dataset's alignment — not merely that
		// *some* midnight-UTC-aligned Calendar was configured. Verify
		// that claim directly rather than recording it unconditionally:
		// a Manager left at the default FXCalendar (or supplied some
		// other Calendar) must fail clearly here, not either produce a
		// confusing per-record misalignment rejection with no obvious
		// cause, or — worse, for some other calendar that happened to
		// also anchor to midnight UTC — a Manifest that names the wrong
		// Calendar (PR #310 review).
		if _, ok := m.calendar.(*USEquityCalendar); !ok {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", fmt.Errorf(
				"marketdata: stooq: %w: provider \"stooq\" requires Config.Calendar to be a *USEquityCalendar, got %T",
				ErrInvalidConfig, m.calendar)
		}
		snapshot, err := stooq.ReadPartitionSnapshot(ctx, m.rawRoot, symbol, action.Year, action.Month)
		if err != nil {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", fmt.Errorf("read raw partition: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", err
		}
		normalized, err := normalizeStooqSequence(m.calendar, snapshot.Records)
		if err != nil {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", fmt.Errorf("normalize: %w", err)
		}
		// AdjustmentSplitAdjusted: Stooq's own daily equity history is
		// split-adjusted (confirmed empirically against AAPL's real
		// splits, issue #298) but not dividend-adjusted — see
		// AdjustmentSplitAdjusted's own doc comment. Stooq has no feed
		// concept (issue #324, EQ-11): Manifest.Feed is empty for a
		// Stooq-sourced dataset.
		return normalized, snapshot.Fingerprint, BasisTrade, AdjustmentSplitAdjusted, "", calendarVersionUSEquityV1, nil

	case "alpaca":
		if rawInterval != string(alpaca.RawD1) {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", fmt.Errorf("marketdata: alpaca: only %s is supported, got %s", D1, action.Interval)
		}
		// Same verification stooq's own case above performs, and for the
		// identical reason (PR #310 review): a Manifest.CalendarVersion
		// naming USEquityCalendar is a specific claim about which
		// Calendar implementation actually validated this dataset's
		// alignment, not merely that some midnight-UTC-aligned Calendar
		// was configured.
		if _, ok := m.calendar.(*USEquityCalendar); !ok {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", fmt.Errorf(
				"marketdata: alpaca: %w: provider \"alpaca\" requires Config.Calendar to be a *USEquityCalendar, got %T",
				ErrInvalidConfig, m.calendar)
		}
		snapshot, err := alpaca.ReadPartitionSnapshot(ctx, m.rawRoot, symbol, action.Year, action.Month)
		if err != nil {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", fmt.Errorf("read raw partition: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", err
		}
		normalized, err := normalizeAlpacaSequence(m.calendar, snapshot.Records)
		if err != nil {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", fmt.Errorf("normalize: %w", err)
		}
		// AdjustmentSplitAdjusted, matching Stooq's own choice above, for
		// cross-provider comparability of the same instrument: Alpaca's
		// bars endpoint is requested with adjustment=split (see
		// alpaca.Client.fetchAllPages, issue #323's SDK migration), which
		// is Alpaca's own split-adjusted-but-not-dividend-adjusted
		// convention — the same shape AdjustmentSplitAdjusted's own doc
		// comment already describes for Stooq. calendarVersionUSEquityV1
		// is reused unchanged: both providers validate against the exact
		// same USEquityCalendar D1 boundary (midnight UTC), so there is no
		// reason to mint a second, functionally-identical calendar
		// version string. snapshot.Feed (issue #324, EQ-11) records which
		// Alpaca feed (IEX/SIP) actually produced this raw partition's
		// data, read back from the partition's own schema comment rather
		// than assumed from the client's current configuration — the two
		// can disagree if the client's configured feed changed since this
		// partition was last fetched (syncOneAlpaca's own feed-mismatch
		// guard, sync.go, prevents that from ever silently mixing two
		// feeds' data into one partition, but does not prevent an
		// operator from later reading an older partition with a
		// differently-configured Manager).
		return normalized, snapshot.Fingerprint, BasisTrade, AdjustmentSplitAdjusted, string(snapshot.Feed), calendarVersionUSEquityV1, nil

	default:
		// ReadPartitionSnapshot, not separate ReadPartitionRecords/
		// FingerprintPartition calls: those would open the raw file
		// twice, admitting a window in which Sync atomically replaces
		// it in between, so the records normalized below and the
		// fingerprint recorded on the published Manifest could end up
		// describing two different revisions of the raw file (see
		// ReadPartitionSnapshot's own doc comment). One read guarantees
		// they always describe the same bytes.
		oandaInterval := oanda.RawInterval(rawInterval)
		snapshot, err := oanda.ReadPartitionSnapshot(ctx, m.rawRoot, symbol, oandaInterval, action.Year, action.Month)
		if err != nil {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", fmt.Errorf("read raw partition: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", err
		}
		normalized, err := normalizeOANDASequence(oandaInterval, m.calendar, snapshot.Records)
		if err != nil {
			return nil, "", BasisUnknown, AdjustmentUnknown, "", "", fmt.Errorf("normalize: %w", err)
		}
		return normalized, snapshot.Fingerprint, BasisBid, AdjustmentNotApplicable, "", calendarVersionCurrent, nil
	}
}
