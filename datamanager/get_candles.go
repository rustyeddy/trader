package datamanager

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// candleWindowBufferNum/candleWindowBufferDen pad the calendar span
// requested from Candles() to account for FX markets being closed on
// weekends (and, less predictably, holidays): a trading week only covers 5
// of 7 calendar days, so a naive count*timeframe span would come up short.
// Expressed as an integer ratio rather than a float per this codebase's
// no-internal-floats rule (see CLAUDE.md). Matches — does not yet replace,
// see docs/archive/asof-review-sweep-spec.md §3 — the 1.4x factor
// service/review.go's reviewWindow already uses for "D" and "H4"
// granularities (7/5 == 1.4).
const (
	candleWindowBufferNum = 7
	candleWindowBufferDen = 5
)

// candleWindowSeconds returns the calendar span, in seconds, needed to
// fetch at least count valid candles at the given timeframe, buffered per
// candleWindowBufferNum/candleWindowBufferDen and rounded up so the window
// is never short by a fractional second.
func candleWindowSeconds(tf types.Timeframe, count int) int64 {
	span := int64(tf) * int64(count) * candleWindowBufferNum
	return (span + candleWindowBufferDen - 1) / candleWindowBufferDen
}

// CandleWindow returns the calendar duration needed to fetch at least count
// valid candles at the given timeframe, buffered for weekends/holidays.
// GetCandles computes this internally and callers do not need it just to
// call GetCandles; it's exported for callers (like service/review.go) that
// separately need to know how far back to look — e.g. to ensure the local
// store has enough history before calling GetCandles, or to size a
// direct-source fallback fetch when the local store falls short.
func CandleWindow(tf types.Timeframe, count int) time.Duration {
	return time.Duration(candleWindowSeconds(tf, count)) * time.Second
}

// GetCandles returns the most recent count valid (market-session-only,
// gap-compacted), timestamped candles for req.Instrument/req.Range.TF at or
// before asof. Unlike Candles(), which returns a streaming iterator for
// sequential replay, GetCandles is for callers (like review/) that need
// random access — the last candle, the last N candles — and are willing to
// materialize the full result eagerly.
//
// Timestamps are included (market.CandleTime, not bare market.Candle)
// because callers like review's weekly-candle derivation need to bucket
// daily candles by calendar week; stripping timestamps here would make
// that impossible to recover afterwards, since closed-market gaps mean a
// candle's position in the slice doesn't determine its date.
//
// req.Range is ignored on input and overwritten with a range computed from
// asof and count: a window wide enough to cover count valid candles ending
// at or before asof, per candleWindowSeconds. Only req.Instrument,
// req.Source, req.Range.TF, and req.Strict are read from req.
//
// The returned slice is already compacted to valid, market-session candles
// — closed-market calendar slots (weekends, holidays) are never included —
// since Candles() already filters them via the underlying candleSetIterator.
func (dm *DataManager) GetCandles(ctx context.Context, req CandleRequest, asof time.Time, count int) ([]market.CandleTime, error) {
	if count <= 0 {
		return nil, fmt.Errorf("GetCandles: count must be positive, got %d", count)
	}

	tf := req.timeframe()
	end := types.FromTime(asof) + 1 // +1: End is exclusive, and asof itself must be included
	start := max(end-types.Timestamp(candleWindowSeconds(tf, count)), 1)
	req.Range = types.TimeRange{Start: start, End: end, TF: tf}

	iter, err := dm.Candles(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	candles := make([]market.CandleTime, 0, count)
	for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
		if ct.Candle.IsZero() {
			// The Valid bitset only reflects the on-disk flag byte, not the
			// actual OHLC content — a corrupt or partially-written CSV row
			// can be flagged valid yet hold a zero-value candle. Skip it so
			// a short/corrupt cache comes back short of count rather than
			// silently returning unusable data, matching the callers this
			// replaced (e.g. service/review.go's short-cache fallback to a
			// direct OANDA fetch).
			continue
		}
		candles = append(candles, ct)
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}

	if len(candles) > count {
		candles = candles[len(candles)-count:]
	}
	return candles, nil
}
