package main

import (
	"fmt"
	"iter"
	"time"
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
func runATRStudy(bars iter.Seq2[bar, error], window int) ([]atrObservation, error) {
	indicator := newATR(window)
	var observations []atrObservation
	for b, err := range bars {
		if err != nil {
			return nil, fmt.Errorf("atr study: reading bars: %w", err)
		}
		if value, ok := indicator.onBar(b); ok {
			observations = append(observations, atrObservation{instrument: b.instrument, opened: b.Time, value: value})
		}
	}
	return observations, nil
}
