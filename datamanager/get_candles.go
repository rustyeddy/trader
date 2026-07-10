package datamanager

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/market"
)

// candleWindowBufferNum/candleWindowBufferDen pad the calendar span
// requested from Candles() to account for FX markets being closed on
// weekends (and, less predictably, holidays): a trading week only covers 5
// of 7 calendar days, so a naive count*timeframe span would come up short.
// Expressed as an integer ratio rather than a float per this codebase's
// no-internal-floats rule (see CLAUDE.md). Matches — does not yet replace,
// see docs/asof-review-sweep-spec.md §3 — the 1.4x factor
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
func candleWindowSeconds(tf market.Timeframe, count int) int64 {
	span := int64(tf) * int64(count) * candleWindowBufferNum
	return (span + candleWindowBufferDen - 1) / candleWindowBufferDen
}

// GetCandles returns the most recent count valid (market-session-only,
// gap-compacted) candles for req.Instrument/req.Range.TF at or before asof.
// Unlike Candles(), which returns a streaming iterator for sequential
// replay, GetCandles is for callers (like review/) that need random access
// — the last candle, the last N candles — and are willing to materialize
// the full result eagerly.
//
// req.Range is ignored on input and overwritten with a range computed from
// asof and count: a window wide enough to cover count valid candles ending
// at or before asof, per candleWindowSeconds. Only req.Instrument,
// req.Source, req.Range.TF, and req.Strict are read from req.
//
// The returned slice is already compacted to valid, market-session candles
// — closed-market calendar slots (weekends, holidays) are never included —
// since Candles() already filters them via the underlying candleSetIterator.
func (dm *DataManager) GetCandles(ctx context.Context, req CandleRequest, asof time.Time, count int) ([]market.Candle, error) {
	if count <= 0 {
		return nil, fmt.Errorf("GetCandles: count must be positive, got %d", count)
	}

	tf := req.timeframe()
	end := market.FromTime(asof) + 1 // +1: End is exclusive, and asof itself must be included
	start := max(end-market.Timestamp(candleWindowSeconds(tf, count)), 1)
	req.Range = market.TimeRange{Start: start, End: end, TF: tf}

	iter, err := dm.Candles(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	candles := make([]market.Candle, 0, count)
	for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
		candles = append(candles, ct.Candle)
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}

	if len(candles) > count {
		candles = candles[len(candles)-count:]
	}
	return candles, nil
}
