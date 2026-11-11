package trader

import (
	"fmt"
	"iter"
	"math"
	"time"
)

// trend is how a scanner classifies an instrument. It is a market reading, not
// the direction of a position.
type trend uint8

const (
	noTrend trend = iota
	trendUp
	trendDown
	trendFlat
)

func (t trend) String() string {
	switch t {
	case trendUp:
		return "up"
	case trendDown:
		return "down"
	case trendFlat:
		return "flat"
	}
	return "unknown"
}

// trendReading is one classification, knowable from availableAt on.
type trendReading struct {
	instrument  string
	availableAt time.Time
	trend       trend
}

// trendTolerance is how close two EMAs must be to count as equal, about
// 1/10,000 of a price point. The EMAs are floats, so a constant price can seed
// them a rounding error apart; that is not a trend.
const trendTolerance = 1e-9

// classifyTrend compares a short EMA with a long one.
func classifyTrend(short, long float64) trend {
	switch {
	case math.Abs(short-long) <= trendTolerance:
		return trendFlat
	case short > long:
		return trendUp
	}
	return trendDown
}

// emaPair is one instrument's indicator state.
type emaPair struct {
	short, long *ema
}

// runEMAScan classifies each bar's instrument by its short and long EMAs, in the
// order the bars arrive, which mergedBars makes deterministic. Each instrument
// has its own EMAs and warms up on its own: it has no reading until its long EMA
// is seeded. A bar that is missing gives no reading and does not advance the
// EMAs. It needs no trader, broker or account. Cancellation arrives through
// bars, as an error. On any error it returns no readings, never a partial list.
func runEMAScan(bars iter.Seq2[bar, error], short, long int) ([]trendReading, error) {
	if short <= 0 || short >= long {
		return nil, fmt.Errorf("ema scan: need 0 < short < long (short=%d long=%d)", short, long)
	}

	pairs := make(map[string]*emaPair)
	var readings []trendReading
	for b, err := range bars {
		if err != nil {
			return nil, fmt.Errorf("ema scan: reading bars: %w", err)
		}
		pair := pairs[b.instrument]
		if pair == nil {
			pair = &emaPair{short: newEMA(short), long: newEMA(long)}
			pairs[b.instrument] = pair
		}

		// Feed both every bar, so the long one warms up on the same data.
		s, sok := pair.short.onBar(b)
		l, lok := pair.long.onBar(b)
		if sok && lok {
			readings = append(readings, trendReading{b.instrument, b.availableAt, classifyTrend(s, l)})
		}
	}
	return readings, nil
}
