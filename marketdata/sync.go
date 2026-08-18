package marketdata

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/rustyeddy/trader/instrument"
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
	if m.oandaClient == nil {
		return SyncResult{}, fmt.Errorf("marketdata: sync: %w: OANDA credential/base URL is not configured", ErrInvalidConfig)
	}
	if m.rawRoot == "" {
		return SyncResult{}, fmt.Errorf("marketdata: sync: %w: raw root is not configured", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return SyncResult{}, err
	}

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
		dr, err := m.syncOne(ctx, action)
		if err != nil {
			return result, fmt.Errorf("marketdata: sync: %s %s %04d-%02d: %w",
				action.Instrument, action.Interval, action.Year, int(action.Month), err)
		}
		result.Downloaded = append(result.Downloaded, dr)
	}
	return result, nil
}

// syncOne executes a single ActionDownloadRaw entry, per Sync's own
// "missing versus extend, decided operationally" rule.
func (m *Manager) syncOne(ctx context.Context, action Action) (DownloadResult, error) {
	rawInterval, ok := intervalToRawInterval(action.Interval)
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

// resolveRawSymbol resolves id to this Manager's provider-native display
// symbol, the same way Bars/Coverage/Plan already do.
func (m *Manager) resolveRawSymbol(id instrument.ID) (string, error) {
	listing, err := m.resolver.ResolveInstrument(id, m.providerName, "")
	if err != nil {
		return "", fmt.Errorf("resolve listing: %w", err)
	}
	return listing.Symbol(), nil
}
