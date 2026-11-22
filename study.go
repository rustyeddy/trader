package trader

import (
	"fmt"
	"iter"
	"time"

	"github.com/rustyeddy/trader/indicators"
	"github.com/rustyeddy/trader/marketdata"
)

// atrObservation is one ATR measurement. opened is the opening time of the bar
// that completed it, so the value is not known until that bar has closed.
type atrObservation struct {
	instrument string
	opened     time.Time
	value      float64
}

// runATRStudy measures the ATR of one instrument's bars, in order, and returns
// a measurement for every bar from the first one that has a full window. It
// needs no trader, broker or account. Cancellation arrives through bars, as an
// error. On any error it returns no measurements, never a partial series.
func runATRStudy(bars iter.Seq2[marketdata.Bar, error], window int) ([]atrObservation, error) {
	indicator, err := indicators.NewATR(window)
	if err != nil {
		return nil, fmt.Errorf("atr study: %w", err)
	}
	var observations []atrObservation
	for b, err := range bars {
		if err != nil {
			return nil, fmt.Errorf("atr study: reading bars: %w", err)
		}
		px := b.OHLC()
		if err := indicator.Update(px.High, px.Low, px.Close); err != nil {
			return nil, fmt.Errorf("atr study: %w", err)
		}
		if indicator.Ready() {
			observations = append(observations, atrObservation{instrument: b.Instrument, opened: b.Time, value: indicator.Value()})
		}
	}
	return observations, nil
}
