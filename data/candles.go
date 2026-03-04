package data

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

func (df *datafile) hourStart() types.Timestamp {
	t := df.Time.UTC()
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	return types.Timestamp(t.Unix())
}

// NOTE: If Tick.Timestamp is already unix seconds, remove the /1000 conversion below.
// This implementation assumes Tick.Timestamp is unix milliseconds.
func (df *datafile) buildM1(ctx context.Context) (*market.CandleSet, error) {
	const minutesPerHour = 60
	hourStart := df.hourStart()

	cs := &market.CandleSet{
		Instrument: market.Instruments[df.symbol], // adjust if your lookup differs
		Start:      types.Timestamp(hourStart),
		Timeframe:  60,
		Scale:      1_000_000,
		Source:     "Dukascopy M1",
		Candles:    make([]market.Candle, minutesPerHour),
		Valid:      make([]uint64, (minutesPerHour+63)/64),
	}

	var (
		curIdx        = -1
		cur           market.Candle
		spreadSum     int64
		havePrevClose bool
		prevClose     types.Price
	)

	finalize := func() error {
		if curIdx < 0 {
			return nil
		}
		if cur.Ticks <= 0 {
			return nil
		}
		ticks := int64(cur.Ticks)
		cur.AvgSpread = types.Price((spreadSum + ticks/2) / ticks)

		cs.Candles[curIdx] = cur
		bitSet(cs.Valid, curIdx) // use your local/exported helper

		prevClose = cur.Close
		havePrevClose = true
		return nil
	}

	fillFlat := func(idx int, px types.Price) {
		// Fill OHLC but do NOT set Valid bit.
		cs.Candles[idx] = market.Candle{
			Open:  px,
			High:  px,
			Low:   px,
			Close: px,
			Ticks: 0,
		}
	}

	err := df.forEachTick(ctx, func(t Tick) error {
		ts := t.Timestamp
		if ts <= 0 {
			return fmt.Errorf("bad tick timestamp: %d", t.Timestamp)
		}

		minuteOpen := ts.FloorToMinute()
		idx := int((minuteOpen - hourStart) / 60)
		if idx < 0 || idx >= minutesPerHour {
			return fmt.Errorf("tick outside hour window: minute=%d hourStart=%d idx=%d",
				minuteOpen, hourStart, idx)
		}

		mid := t.Mid()
		spread := t.Spread()

		if curIdx == -1 {
			curIdx = idx
			cur = market.Candle{
				Open:      mid,
				High:      mid,
				Low:       mid,
				Close:     mid,
				Ticks:     1,
				MaxSpread: spread,
			}
			spreadSum = int64(spread)
			return nil
		}

		if idx == curIdx {
			if mid > cur.High {
				cur.High = mid
			}
			if mid < cur.Low {
				cur.Low = mid
			}
			cur.Close = mid
			cur.Ticks++

			if spread > cur.MaxSpread {
				cur.MaxSpread = spread
			}
			spreadSum += int64(spread)
			return nil
		}

		if idx < curIdx {
			return fmt.Errorf("out-of-order tick minute: idx %d < curIdx %d", idx, curIdx)
		}

		if err := finalize(); err != nil {
			return err
		}

		if havePrevClose {
			for m := curIdx + 1; m < idx; m++ {
				if !bitIsSet(cs.Valid, m) && isZeroCandle(cs.Candles[m]) {
					fillFlat(m, prevClose)
				}
			}
		}

		curIdx = idx
		cur = market.Candle{
			Open:      mid,
			High:      mid,
			Low:       mid,
			Close:     mid,
			Ticks:     1,
			MaxSpread: spread,
		}
		spreadSum = int64(spread)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := finalize(); err != nil {
		return nil, err
	}

	if havePrevClose && curIdx >= 0 {
		for m := curIdx + 1; m < minutesPerHour; m++ {
			if !bitIsSet(cs.Valid, m) && isZeroCandle(cs.Candles[m]) {
				fillFlat(m, prevClose)
			}
		}
	}

	return cs, nil
}

func isZeroCandle(c market.Candle) bool {
	return c.Open == 0 && c.High == 0 && c.Low == 0 && c.Close == 0 && c.Ticks == 0
}

// If you can't access market's bitSet/bitIsSet because they are unexported,
// include these tiny helpers in the data package (or export them from market).
func bitIsSet(bits []uint64, idx int) bool {
	return (bits[idx>>6] & (1 << uint(idx&63))) != 0
}
func bitSet(bits []uint64, idx int) {
	bits[idx>>6] |= 1 << uint(idx&63)
}
