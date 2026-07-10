package datamanager

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/market"
)

// candleWindowBufferFactor pads the calendar span requested from Candles()
// to account for FX markets being closed on weekends (and, less
// predictably, holidays): a trading week only covers 5 of 7 calendar days,
// so a naive count*timeframe span would come up short. Relocated from
// service/review.go's reviewWindow per docs/asof-review-sweep-spec.md §2 —
// same 1.4x factor reviewWindow already used for "D" and "H4" granularities.
const candleWindowBufferFactor = 1.4

// candleWindowDuration returns the calendar duration needed to fetch at
// least count valid candles at the given timeframe, buffered per
// candleWindowBufferFactor.
func candleWindowDuration(tf market.Timeframe, count int) time.Duration {
	return time.Duration(float64(tf) * float64(count) * candleWindowBufferFactor * float64(time.Second))
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
// at or before asof, per candleWindowDuration. Only req.Instrument,
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
	start := max(end-market.Timestamp(candleWindowDuration(tf, count)/time.Second), 1)
	req.Range = market.TimeRange{Start: start, End: end, TF: tf}

	iter, err := dm.Candles(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	var candles []market.Candle
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
