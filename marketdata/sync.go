package marketdata

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata/internal/provider/alpaca"
	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
)

// DownloadResult reports the outcome of executing one ActionDownloadRaw
// entry.
type DownloadResult struct {
	Action Action
	// RecordsWritten is the total number of records the resulting raw
	// partition file holds after this Sync call — the full file's
	// content, not just the newly-fetched tail for an "extend".
	RecordsWritten int
}

// SkippedAction records one Plan Action Sync did not execute, and why —
// see Sync's doc comment. A Skipped entry is not a failure: Sync still
// succeeds and continues to the next Action.
type SkippedAction struct {
	Action Action
	Reason string
}

// SyncResult summarizes one Manager.Sync call: every Action executed and
// every Action skipped, so a caller can account for the full Plan it
// supplied rather than only ever seeing the successful subset.
type SyncResult struct {
	Downloaded []DownloadResult
	Skipped    []SkippedAction
}

// Sync executes exactly the ActionDownloadRaw entries in plan — never a
// caller-supplied instrument/interval/range, and never its own
// recomputed plan — acquiring missing or extending raw OANDA partitions
// while leaving canonical data and the canonical store completely
// untouched (issue #80, ADR-020). A plan is normally obtained from a
// prior Manager.Plan call; Sync does not require that plan is fresh
// relative to Coverage/Plan's own read of the world, but nor does it
// re-verify Coverage before acting — a caller wanting an up-to-date plan
// should call Plan again immediately before Sync.
//
// # Only raw downloads
//
// ActionNormalizeCanonical and ActionDeriveCanonical entries are
// reported in SyncResult.Skipped, not executed and not silently
// dropped: building canonical data from raw is a separate, future
// Manager operation. This keeps Sync's own scope exactly what issue
// #80 asked for — fill missing raw partitions and extend recent
// history — auditable rather than implicit.
//
// # Missing versus extend, decided operationally
//
// Sync does not trust Action.Reason's text to decide how to write a
// partition (Reason is diagnostic, not a machine dispatch key). Instead,
// for each ActionDownloadRaw entry it checks whether a raw partition
// file already exists for that (instrument, interval, year, month):
//
//   - No file: the entire calendar month (or through now, whichever is
//     earlier) is fetched and written as a brand-new file
//     (WritePartition's mustNotExist), which errors rather than silently
//     overwriting anything if a file unexpectedly appears there between
//     the check and the write.
//   - An existing, readable file: only the range from its last record
//     forward (through now or month's end, whichever is earlier) is
//     fetched, merged with the existing records, and the file is
//     atomically replaced with the merged result — never a bulk
//     redownload of already-preserved history.
//   - An existing but unreadable/malformed file: Sync fails loudly for
//     that Action rather than guessing; repairing a corrupted raw
//     partition is the explicit, separate operation issue #80's own
//     scope reserves for later, not something an ordinary sync performs
//     automatically.
//
// # Credentials
//
// The bearer token never appears in any DownloadResult, SkippedAction,
// or error Sync returns — see oanda.Client and oanda.CredentialProvider,
// which own that guarantee structurally (the token is placed on exactly
// one outgoing request header and nowhere else).
func (m *Manager) Sync(ctx context.Context, plan Plan) (SyncResult, error) {
	if !m.configured() {
		return SyncResult{}, fmt.Errorf("marketdata: sync: %w: manager is not configured", ErrInvalidConfig)
	}
	if m.rawRoot == "" {
		return SyncResult{}, fmt.Errorf("marketdata: sync: %w: raw root is not configured", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return SyncResult{}, err
	}

	// requireSyncClient is checked only once a real ActionDownloadRaw is
	// about to execute, not unconditionally up front: a plan containing
	// only normalize/build work (or no work at all) has nothing for a
	// provider acquisition client to do, and must not fail merely
	// because none is configured — this was a real bug (PR #312
	// review), most visible for provider "stooq", which never has a
	// live acquisition client at all yet legitimately calls Sync with
	// an empty or download-free plan.
	var clientChecked bool

	var result SyncResult
	for _, action := range plan.Actions {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if action.Kind != ActionDownloadRaw {
			result.Skipped = append(result.Skipped, SkippedAction{
				Action: action,
				Reason: fmt.Sprintf("%s is not a raw download; executing it is a future, separate Manager operation", action.Kind),
			})
			continue
		}
		if !clientChecked {
			if err := m.requireSyncClient(); err != nil {
				return result, err
			}
			clientChecked = true
		}
		dr, err := m.syncOne(ctx, action)
		if err != nil {
			return result, fmt.Errorf("marketdata: sync: %s %s %04d-%02d: %w",
				action.Instrument, action.Interval, action.Year, int(action.Month), err)
		}
		result.Downloaded = append(result.Downloaded, dr)
	}
	return result, nil
}

// requireSyncClient reports a clear, wrapped ErrInvalidConfig if this
// Manager's provider (m.providerName) has no acquisition client
// configured to execute an ActionDownloadRaw entry — dispatched by
// provider, the same seam ADR-047 already established for
// rawInventoryLookup/readAndNormalizeRaw, now extended to Sync, which
// previously checked m.oandaClient unconditionally regardless of
// provider. OANDA's own error text and behavior are unchanged.
func (m *Manager) requireSyncClient() error {
	switch m.providerName {
	case "alpaca":
		if m.alpacaClient == nil {
			return fmt.Errorf("marketdata: sync: %w: Alpaca credential/base URL is not configured", ErrInvalidConfig)
		}
	case "stooq":
		// Stooq is a one-shot offline import (stooq.Import) with no live
		// acquisition client at all — the same fact allowsLiveExtend
		// already records for Plan. Sync has nothing it could call to
		// execute an ActionDownloadRaw entry for this provider, so it
		// fails clearly here rather than misreporting a missing OANDA/
		// Alpaca credential that was never relevant.
		return fmt.Errorf("marketdata: sync: %w: provider \"stooq\" has no live acquisition client; use stooq.Import to add raw history", ErrInvalidConfig)
	default:
		if m.oandaClient == nil {
			return fmt.Errorf("marketdata: sync: %w: OANDA credential/base URL is not configured", ErrInvalidConfig)
		}
	}
	return nil
}

// syncOne executes a single ActionDownloadRaw entry, dispatched to the
// concrete provider implementation named by m.providerName (ADR-047's
// internal provider seam, extended to Sync). OANDA's own syncOneOANDA
// is byte-for-byte the previous syncOne body, entirely unchanged.
func (m *Manager) syncOne(ctx context.Context, action Action) (DownloadResult, error) {
	if m.providerName == "alpaca" {
		return m.syncOneAlpaca(ctx, action)
	}
	return m.syncOneOANDA(ctx, action)
}

// syncOneOANDA executes a single ActionDownloadRaw entry against OANDA,
// per Sync's own "missing versus extend, decided operationally" rule.
func (m *Manager) syncOneOANDA(ctx context.Context, action Action) (DownloadResult, error) {
	rawIntervalToken, ok := intervalToRawInterval(action.Interval)
	if !ok {
		return DownloadResult{}, fmt.Errorf("interval %s has no raw partition", action.Interval)
	}
	symbol, err := m.resolveRawSymbol(action.Instrument)
	if err != nil {
		return DownloadResult{}, err
	}

	monthStart := time.Date(action.Year, action.Month, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	upper := monthEnd
	if now := m.clock.Now(); now.Before(upper) {
		upper = now
	}

	rawInterval := oanda.RawInterval(rawIntervalToken)
	existing, err := oanda.ReadPartitionRecords(ctx, m.rawRoot, symbol, rawInterval, action.Year, action.Month)
	mustNotExist := false
	from := monthStart
	switch {
	case errors.Is(err, fs.ErrNotExist):
		mustNotExist = true
	case err != nil:
		return DownloadResult{}, fmt.Errorf("read existing raw partition: %w", err)
	default:
		// A design review's finding: this must not simply trust file
		// order for "last." ReadPartitionRecords returns records in
		// file order, and nothing on the read path validates that a
		// pre-existing file is actually Time-ordered (a hand-edited or
		// otherwise irregular file could still parse cleanly); sorting
		// here, not just on write, is what keeps "the record with the
		// greatest Time" honest regardless of how the file on disk got
		// that way.
		sort.Slice(existing, func(i, j int) bool { return existing[i].Time.Before(existing[j].Time) })
		if n := len(existing); n > 0 {
			last := existing[n-1]
			if last.Complete {
				from = last.Time.Add(time.Nanosecond)
			} else {
				// A second design-review finding: an incomplete tail
				// candle must be re-fetched from its own Time, not
				// skipped past with +1ns — OANDA may still finalize its
				// OHLC/volume, and advancing past it would freeze the
				// provisional values in place permanently. The refetch
				// below is merged back in by Time, so the refreshed
				// record replaces the stale provisional one rather than
				// duplicating it.
				from = last.Time
			}
		}
	}

	merged := existing
	if upper.After(from) {
		fetched, err := m.oandaClient.FetchCandles(ctx, oanda.CandleRequest{
			Symbol: symbol, Interval: rawInterval, From: from, To: upper,
		})
		if err != nil {
			return DownloadResult{}, fmt.Errorf("fetch candles: %w", err)
		}
		if len(fetched) > 0 || mustNotExist {
			merged = mergeRecordsByTime(existing, fetched)
			if err := oanda.WritePartition(ctx, m.rawRoot, symbol, rawInterval, action.Year, action.Month, merged, mustNotExist); err != nil {
				return DownloadResult{}, fmt.Errorf("write partition: %w", err)
			}
		}
	}

	return DownloadResult{Action: action, RecordsWritten: len(merged)}, nil
}

// mergeRecordsByTime combines existing and fetched into one set with at
// most one Record per distinct Time: fetched always wins a collision.
// This is what lets a re-fetched, now-finalized candle replace a stale
// provisional one from an earlier sync rather than appearing twice —
// see syncOne's incomplete-tail handling above. The result is unsorted;
// WritePartition sorts before writing.
func mergeRecordsByTime(existing, fetched []oanda.Record) []oanda.Record {
	byTime := make(map[int64]oanda.Record, len(existing)+len(fetched))
	for _, r := range existing {
		byTime[r.Time.UTC().UnixNano()] = r
	}
	for _, r := range fetched {
		byTime[r.Time.UTC().UnixNano()] = r
	}
	out := make([]oanda.Record, 0, len(byTime))
	for _, r := range byTime {
		out = append(out, r)
	}
	return out
}

// syncOneAlpaca executes a single ActionDownloadRaw entry against
// Alpaca, mirroring syncOneOANDA's own "missing versus extend" logic.
// It differs from syncOneOANDA in one respect: every alpaca.Record
// already represents a fully closed trading day (see
// alpaca.Partition.LastComplete's own doc comment) — there is no
// provisional/incomplete tail record analogous to an OANDA candle
// whose Complete flag might still be false, so there is no
// incomplete-tail re-fetch case to handle; advancing strictly past the
// last existing record's own Time is always correct.
func (m *Manager) syncOneAlpaca(ctx context.Context, action Action) (DownloadResult, error) {
	rawIntervalToken, ok := intervalToRawInterval(action.Interval)
	if !ok {
		return DownloadResult{}, fmt.Errorf("interval %s has no raw partition", action.Interval)
	}
	if rawIntervalToken != string(alpaca.RawD1) {
		return DownloadResult{}, fmt.Errorf("marketdata: alpaca: only %s is supported, got %s", D1, action.Interval)
	}
	symbol, err := m.resolveRawSymbol(action.Instrument)
	if err != nil {
		return DownloadResult{}, err
	}
	cal, ok := m.calendar.(*USEquityCalendar)
	if !ok {
		return DownloadResult{}, fmt.Errorf(
			"marketdata: alpaca: %w: provider \"alpaca\" requires Config.Calendar to be a *USEquityCalendar, got %T",
			ErrInvalidConfig, m.calendar)
	}

	monthStart := time.Date(action.Year, action.Month, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	upper := monthEnd
	if now := m.clock.Now(); now.Before(upper) {
		upper = now
	}
	// Never request through a trading day whose regular session has
	// not actually closed yet: every alpaca.Record is treated as a
	// fully closed, final daily bar (see syncOneAlpaca's own doc
	// comment), so persisting the current, still-forming trading day's
	// bar mid-session would silently canonicalize provisional data as
	// settled history (PR #312 review). RegularSessionEnd answers this
	// regardless of whether upper itself falls inside or outside real
	// trading hours, unlike Session/Status.
	today := time.Date(upper.Year(), upper.Month(), upper.Day(), 0, 0, 0, 0, time.UTC)
	if sessionEnd, isTradingDay := cal.RegularSessionEnd(upper); isTradingDay && upper.Before(sessionEnd) {
		upper = today
	}

	snapshot, err := alpaca.ReadPartitionSnapshot(ctx, m.rawRoot, symbol, action.Year, action.Month)
	mustNotExist := false
	from := monthStart
	existing := snapshot.Records
	existingFeed := snapshot.Feed
	switch {
	case errors.Is(err, fs.ErrNotExist):
		mustNotExist = true
	case err != nil:
		return DownloadResult{}, fmt.Errorf("read existing raw partition: %w", err)
	default:
		sort.Slice(existing, func(i, j int) bool { return existing[i].Time.Before(existing[j].Time) })
		if n := len(existing); n > 0 {
			from = existing[n-1].Time.Add(time.Nanosecond)
		}
	}

	// A non-empty existingFeed disagreeing with the client's own
	// configured feed means this month's raw partition was fetched
	// under a different feed than the one about to extend it — IEX and
	// SIP are not directly comparable data for the same symbol/date
	// (ADR-050/052), so silently appending one feed's bars onto
	// another's would mix two different datasets under one partition
	// identity (issue #324, EQ-11's own "do not silently mix" scope
	// item).
	//
	// A partition with existing rows but an empty existingFeed (written
	// before EQ-11 added feed provenance) is refused for extension too,
	// not silently accepted: WritePartition below writes the *entire*
	// merged partition under one feed value, so accepting the extension
	// would relabel those legacy rows' genuinely unknowable provenance
	// as currentFeed — turning an honest "unknown" into a false,
	// concrete claim, and still risking an actual mixed-feed partition
	// if those rows in fact came from the other feed (PR #328 review).
	// The only safe response is to require an explicit re-fetch of the
	// whole partition under a known feed, not an incremental extension.
	// A partition with no existing rows at all (nonexistent, or present
	// but empty) has no provenance to protect and is written under
	// currentFeed normally, regardless of what its own possibly-absent
	// feed metadata said.
	currentFeed := m.alpacaClient.Feed()
	switch {
	case len(existing) == 0:
		// Nothing to conflict with.
	case existingFeed == "":
		return DownloadResult{}, fmt.Errorf(
			"marketdata: alpaca: %w: existing partition %s %04d-%02d has %d row(s) with no recorded feed provenance (written before issue #324) and cannot be safely extended under feed %q — re-fetch this partition from scratch under a known feed instead",
			ErrFeedMismatch, symbol, action.Year, action.Month, len(existing), currentFeed)
	case existingFeed != currentFeed:
		return DownloadResult{}, fmt.Errorf(
			"marketdata: alpaca: %w: existing partition %s %04d-%02d was fetched under feed %q, client is configured for %q",
			ErrFeedMismatch, symbol, action.Year, action.Month, existingFeed, currentFeed)
	}

	merged := existing
	if upper.After(from) {
		fetched, err := m.alpacaClient.FetchBars(ctx, alpaca.BarRequest{Symbol: symbol, From: from, To: upper})
		if err != nil {
			return DownloadResult{}, fmt.Errorf("fetch bars: %w", err)
		}
		if len(fetched) > 0 || mustNotExist {
			merged = mergeAlpacaRecordsByTime(existing, fetched)
			if err := alpaca.WritePartition(ctx, m.rawRoot, symbol, action.Year, action.Month, currentFeed, merged, mustNotExist); err != nil {
				return DownloadResult{}, fmt.Errorf("write partition: %w", err)
			}
		}
	}

	return DownloadResult{Action: action, RecordsWritten: len(merged)}, nil
}

// ErrFeedMismatch marks an attempt to extend an existing, non-empty
// Alpaca raw partition in a way that could mix two different feeds'
// data into one partition file (issue #324, EQ-11): either the
// client's configured feed genuinely disagrees with the partition's
// own recorded feed, or the partition has rows but no recorded feed at
// all (written before this issue existed) and so cannot be proven
// compatible with the client's configured feed either way. IEX and SIP
// are not directly comparable data for the same symbol/date
// (ADR-050/052); this error exists so that difference is never
// silently mixed into one partition file, and so legacy rows with
// genuinely unknowable provenance are never relabeled with a concrete
// feed just because that happens to be the feed used to extend them.
var ErrFeedMismatch = errors.New("marketdata: alpaca feed mismatch")

// mergeAlpacaRecordsByTime is mergeRecordsByTime's alpaca.Record
// counterpart: at most one Record per distinct Time, with fetched
// always winning a collision. Unlike oanda.WritePartition (which
// sorts internally, so mergeRecordsByTime's own unsorted map-iteration
// output is harmless), alpaca.WritePartition deliberately does *not*
// sort — it preserves the exact order it is given, the same "preserve
// provider-native order, judge it at normalization" discipline
// stooq.WritePartition already established (issue #303/#306's own
// review finding: silently sorting on write would make an out-of-order
// input undetectable at normalization). Returning here in map-iteration
// (nondeterministic) order would therefore let Sync itself write a
// genuinely out-of-order raw partition — not a hypothetical, a real bug
// (PR #312 review) — so the merged result is explicitly sorted by Time
// before being returned, guaranteeing a well-ordered partition on every
// extend regardless of iteration order.
func mergeAlpacaRecordsByTime(existing, fetched []alpaca.Record) []alpaca.Record {
	byTime := make(map[int64]alpaca.Record, len(existing)+len(fetched))
	for _, r := range existing {
		byTime[r.Time.UTC().UnixNano()] = r
	}
	for _, r := range fetched {
		byTime[r.Time.UTC().UnixNano()] = r
	}
	out := make([]alpaca.Record, 0, len(byTime))
	for _, r := range byTime {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// resolveRawSymbol resolves id to this Manager's provider-native display
// symbol, the same way Bars/Coverage/Plan already do.
func (m *Manager) resolveRawSymbol(id instrument.ID) (string, error) {
	listing, err := m.resolver.ResolveInstrument(id, m.providerName, "")
	if err != nil {
		return "", fmt.Errorf("resolve listing: %w", err)
	}
	return listing.Symbol(), nil
}
