package datamanager

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// WriteMonthlyCandles writes a slice of Candle as a monthly CSV file in the
// canonical trader format. The candles should be dense (one slot per
// timeframe step within the month); zero-valued candles are treated as gaps.
//
// Source is the data source name (e.g. "oanda", "dukascopy") and ends up in
// the path: <basedir>/<source>/<instrument>/<year>/<month>/<instr>-<year>-<month>-<tf>.csv
func (s *store) WriteMonthlyCandles(source, instrument string, tf types.Timeframe, monthStart time.Time, candles []market.Candle) error {
	if s == nil {
		return fmt.Errorf("nil store")
	}
	if instrument == "" {
		return fmt.Errorf("empty instrument")
	}
	if tf <= 0 {
		return fmt.Errorf("invalid timeframe %d", tf)
	}
	if len(candles) == 0 {
		return fmt.Errorf("no candles to write")
	}

	monthStart = monthStart.UTC()
	cs, err := NewMonthlyCandleSet(
		market.NormalizeInstrument(instrument),
		tf,
		types.FromTime(monthStart),
		types.PriceScale,
		source,
	)
	if err != nil {
		return err
	}
	if len(candles) > len(cs.Candles) {
		return fmt.Errorf("wrong candle count for %s %s %s: got %d max %d",
			cs.Instrument, monthStart.Format("2006-01"), tf, len(candles), len(cs.Candles))
	}

	copy(cs.Candles, candles)

	for i := range candles {
		if !candles[i].IsZero() {
			cs.Valid[i>>6] |= 1 << uint(i&63)
		}
	}

	return s.WriteCSV(cs)
}

// WriteMonthlyCandleTimes is WriteMonthlyCandles for producers that carry
// each candle's true observed open timestamp (market.CandleTime) rather
// than a bare market.Candle. monthStart still selects which calendar
// month's file this is (must be a UTC month boundary, as for
// WriteMonthlyCandles), but the written file's actual first-slot time is
// derived from the candles' own timestamps instead of being assumed to be
// monthStart.
//
// This matters because broker daily-alignment grids (e.g. OANDA's H4/D1,
// anchored to 17:00 America/New_York, DST-dependent) do not begin at UTC
// midnight — reconstructing slot 0's time from monthStart instead of the
// data's own timestamps silently mislabels every candle by the DST offset.
// Every candle's slot index is still assumed evenly spaced by tf from slot
// 0 (true of every broker grid in practice); if two candles disagree on
// where slot 0 falls, that indicates corrupt or misaligned input data and
// is reported as an error rather than silently written.
func (s *store) WriteMonthlyCandleTimes(source, instrument string, tf types.Timeframe, monthStart time.Time, candles []market.CandleTime) error {
	if s == nil {
		return fmt.Errorf("nil store")
	}
	if instrument == "" {
		return fmt.Errorf("empty instrument")
	}
	if tf <= 0 {
		return fmt.Errorf("invalid timeframe %d", tf)
	}
	if len(candles) == 0 {
		return fmt.Errorf("no candles to write")
	}

	monthStart = monthStart.UTC()
	cs, err := NewMonthlyCandleSet(
		market.NormalizeInstrument(instrument),
		tf,
		types.FromTime(monthStart),
		types.PriceScale,
		source,
	)
	if err != nil {
		return err
	}
	if len(candles) > len(cs.Candles) {
		return fmt.Errorf("wrong candle count for %s %s %s: got %d max %d",
			cs.Instrument, monthStart.Format("2006-01"), tf, len(candles), len(cs.Candles))
	}

	step := types.Timestamp(tf)
	haveStart := false
	for i := range candles {
		if candles[i].Candle.IsZero() {
			continue
		}
		trueStart := candles[i].Timestamp - types.Timestamp(i)*step
		if !haveStart {
			cs.Start = trueStart
			haveStart = true
			continue
		}
		if trueStart != cs.Start {
			return fmt.Errorf("inconsistent candle timestamps for %s %s %s: slot %d implies start %d, expected %d",
				cs.Instrument, monthStart.Format("2006-01"), tf, i, trueStart, cs.Start)
		}
	}

	for i := range candles {
		cs.Candles[i] = candles[i].Candle
		if !candles[i].Candle.IsZero() {
			cs.Valid[i>>6] |= 1 << uint(i&63)
		}
	}

	return s.WriteCSV(cs)
}
