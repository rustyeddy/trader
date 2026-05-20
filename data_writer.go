package trader

import (
	"fmt"
	"time"
)

// WriteMonthlyCandles writes a slice of Candle as a monthly CSV file in the
// canonical trader format. The candles should be dense (one slot per
// timeframe step within the month); zero-valued candles are treated as gaps.
//
// Source is the data source name (e.g. "oanda", "dukascopy") and ends up in
// the path: <basedir>/<source>/<instrument>/<year>/<month>/<instr>-<year>-<month>-<tf>.csv
func (s *Store) WriteMonthlyCandles(source, instrument string, tf Timeframe, monthStart time.Time, candles []Candle) error {
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
	cs := &candleSet{
		Instrument: NormalizeInstrument(instrument),
		Start:      FromTime(monthStart),
		Timeframe:  tf,
		Scale:      PriceScale,
		Source:     source,
		Candles:    candles,
		Valid:      make([]uint64, (len(candles)+63)/64),
	}

	for i := range candles {
		if !candles[i].IsZero() {
			cs.Valid[i>>6] |= 1 << uint(i&63)
		}
	}

	return s.WriteCSV(cs)
}
