package marketdata

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
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

	// ReadPartitionSnapshot, not separate ReadPartitionRecords/
	// FingerprintPartition calls: those would open the raw file twice,
	// admitting a window in which Sync atomically replaces it in
	// between, so the records normalized below and the fingerprint
	// recorded on the published Manifest could end up describing two
	// different revisions of the raw file (see ReadPartitionSnapshot's
	// own doc comment). One read guarantees they always describe the
	// same bytes.
	snapshot, err := oanda.ReadPartitionSnapshot(ctx, m.rawRoot, symbol, rawInterval, action.Year, action.Month)
	if err != nil {
		return PublishResult{}, fmt.Errorf("read raw partition: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return PublishResult{}, err
	}

	normalized, err := normalizeOANDASequence(rawInterval, m.calendar, snapshot.Records)
	if err != nil {
		return PublishResult{}, fmt.Errorf("normalize: %w", err)
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

	fingerprint := snapshot.Fingerprint

	monthStart := time.Date(action.Year, action.Month, 1, 0, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(monthStart, monthStart.AddDate(0, 1, 0))
	if err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: %w", err)
	}
	bs := BarSet{Instrument: action.Instrument, Interval: action.Interval, Span: span, Basis: BasisBid, Bars: bars}
	if err := bs.Validate(); err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: assembled bar set: %w", err)
	}

	manifest := Manifest{
		Provider:         m.providerName,
		Instrument:       action.Instrument,
		Interval:         action.Interval,
		Span:             span,
		Basis:            BasisBid,
		SchemaVersion:    canonicalSchemaVersion,
		RawFingerprint:   fingerprint,
		BuilderVersion:   builderVersion,
		ValidatorVersion: validatorVersion,
		ResamplerVersion: noResampler,
		CalendarVersion:  calendarVersionCurrent,
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
