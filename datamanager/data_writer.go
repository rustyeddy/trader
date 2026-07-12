package datamanager

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/market"
)

// WriteMonthlyCandles writes a slice of Candle as a monthly CSV file in the
// canonical trader format. The candles should be dense (one slot per
// timeframe step within the month); zero-valued candles are treated as gaps.
//
// Source is the data source name (e.g. "oanda", "dukascopy") and ends up in
// the path: <basedir>/<source>/<instrument>/<year>/<month>/<instr>-<year>-<month>-<tf>.csv
func (s *store) WriteMonthlyCandles(source, instrument string, tf market.Timeframe, monthStart time.Time, candles []market.Candle) error {
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
		market.FromTime(monthStart),
		market.PriceScale,
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
