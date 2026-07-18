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
// Each candle's timestamp is assigned from monthStart+idx*tf, since callers
// of this function don't carry their own per-candle ground truth (e.g. test
// seeding, synthetic fixtures). Producers that do have real per-candle
// timestamps should use WriteMonthlyCandleTimes instead.
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

	for i := range candles {
		cs.Candles[i].Candle = candles[i] // NewMonthlyCandleSet already set Timestamp
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
// WriteMonthlyCandles); each candle's own Timestamp is stored and written
// verbatim — this is what lets D1/H4 canonical files stay correct across a
// DST transition, where slots aren't evenly spaced from a single anchor.
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

	for i := range candles {
		cs.Candles[i] = candles[i]
		if !candles[i].Candle.IsZero() {
			cs.Valid[i>>6] |= 1 << uint(i&63)
		}
	}

	return s.WriteCSV(cs)
}
