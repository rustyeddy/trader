// Package emax is the EMAX strategy as an external worker writes it: the public
// Go SDK (package strategy) and the public models (marketdata, num), nothing from
// Trader's private packages.
//
// EMAX is long while the short EMA of the close is above the long one and short
// while it is below. A change of side is a close on the crossover bar and the
// opposite open on the next. Every open carries a stop a fixed fraction of the
// price away. The strategy asks only: Trader sizes, admits and fills.
//
// This is the same rule as the EMAX built into Trader (strategy.go, indicators.go
// in the root package), written again here because a worker cannot import it. The
// two are kept honest by a parity test in the root package: the same series, the
// same settings and the same account must give the same orders, fills and result
// through the worker over TCP as through the built-in strategy. If you change the
// rule in either place, that test says so.
package emax

import (
	"fmt"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
)

// MaxPrice is the largest price Trader accepts in an order. An entry whose stop
// would be above it is skipped, as the built-in EMAX skips it.
var MaxPrice = num.MustParsePrice("10000")

// Config is the strategy's settings.
type Config struct {
	Short, Long int       // EMA windows; Short < Long
	StopPct     num.Rate  // the stop's distance from the entry, as a fraction of it: 0 < StopPct < 1
	Tick        num.Price // the instruments' price tick; the stop is a whole number of ticks
}

// Validate reports a setting EMAX cannot run with.
func (c Config) Validate() error {
	switch {
	case c.Short <= 0 || c.Long <= 0:
		return fmt.Errorf("emax: windows must be > 0 (short=%d long=%d)", c.Short, c.Long)
	case c.Short >= c.Long:
		return fmt.Errorf("emax: short window (%d) must be less than long window (%d)", c.Short, c.Long)
	case c.StopPct.Sign() <= 0 || c.StopPct.Cmp(num.MustParseRate("1")) >= 0:
		return fmt.Errorf("emax: stop must be between 0%% and 100%% of price (stop=%s)", c.StopPct)
	case c.Tick.IsZero():
		return fmt.Errorf("emax: no price tick")
	}
	return nil
}

// Strategy is EMAX on each instrument it is shown, with its own EMAs and signal
// state per instrument. It is not safe for concurrent use: a session calls OnBars
// one group at a time.
type Strategy struct {
	cfg Config
	by  map[string]*instrument
}

// New returns a Strategy with the settings, or the reason they will not do.
func New(cfg Config) (*Strategy, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Strategy{cfg: cfg, by: make(map[string]*instrument)}, nil
}

// side is the position the signal is in, or is about to take.
type side string

const (
	flat  side = ""
	long  side = "long"
	short side = "short"
)

// instrument is EMAX's state on one instrument. pos is the last entry signal
// consumed, not the position: a stop-out does not change it, so after one the
// strategy stays flat until the trend turns. pending is the side to open on the
// next bar, after a flipping close.
type instrument struct {
	short, long *EMA
	pos         side
	pending     side
}

// OnBars is the rule for one group of bars, each handed to its instrument alone, so
// it asks for at most one order per bar, in the group's order. The events Trader
// reports are not used: EMAX decides from the bars.
func (s *Strategy) OnBars(bars []marketdata.Bar, _ []strategy.Event) ([]strategy.Order, error) {
	var orders []strategy.Order
	for _, b := range bars {
		st := s.by[b.Instrument]
		if st == nil {
			st = &instrument{short: NewEMA(s.cfg.Short), long: NewEMA(s.cfg.Long)}
			s.by[b.Instrument] = st
		}
		if o, ok := s.onBar(st, b); ok {
			orders = append(orders, o)
		}
	}
	return orders, nil
}

func (s *Strategy) onBar(st *instrument, b marketdata.Bar) (strategy.Order, bool) {
	// Feed both EMAs every bar so the long one warms up on the same data.
	sv, sok := st.short.OnBar(b)
	lv, lok := st.long.OnBar(b)
	if !sok || !lok {
		return strategy.Order{}, false
	}

	// Finish a flip: the close went out last bar, now open the other side.
	if st.pending != flat {
		d := st.pending
		st.pending = flat
		st.pos = d
		return s.open(b, d)
	}

	var trend side
	switch {
	case sv > lv:
		trend = long
	case sv < lv:
		trend = short
	default:
		return strategy.Order{}, false // equal: no change
	}

	switch {
	case trend == st.pos:
		return strategy.Order{}, false
	case st.pos == flat:
		st.pos = trend
		return s.open(b, trend)
	default:
		// In the opposite side: close now, open the new side next bar. A close takes
		// no direction: it acts on the existing position.
		st.pos = flat
		st.pending = trend
		return strategy.Order{Instrument: b.Instrument, Action: "close", Price: b.OHLC().Close}, true
	}
}

// open asks to open d at the bar's close, with the stop. An entry whose stop cannot
// be placed is skipped, and not retried: the next order comes from the next
// crossover.
func (s *Strategy) open(b marketdata.Bar, d side) (strategy.Order, bool) {
	entry := b.OHLC().Close
	stop, err := s.stopFor(entry, d)
	if err != nil || stop.IsZero() || stop.Cmp(MaxPrice) > 0 {
		return strategy.Order{}, false
	}
	return strategy.Order{
		Instrument: b.Instrument,
		Action:     "open",
		Direction:  string(d),
		Price:      entry,
		StopLoss:   stop,
	}, true
}

// stopFor places the stop StopPct of the entry price away on the losing side: below
// a long, above a short. The distance rounds down to the tick but is at least one
// tick.
func (s *Strategy) stopFor(entry num.Price, d side) (num.Price, error) {
	raw, err := entry.MulRate(s.cfg.StopPct)
	if err != nil {
		return num.Price{}, err
	}
	dist, err := raw.RoundDown(s.cfg.Tick)
	if err != nil {
		return num.Price{}, err
	}
	if dist.IsZero() {
		dist = s.cfg.Tick
	}
	if d == short {
		return entry.Add(dist)
	}
	return entry.Sub(dist)
}

// EMA is an exponential moving average of the close, seeded with the simple average
// of the first window closes.
type EMA struct {
	window int
	alpha  float64
	count  int
	sum    float64 // while seeding
	value  float64
}

// NewEMA returns an EMA of the given window, which must be positive.
func NewEMA(window int) *EMA {
	if window <= 0 {
		panic("emax: ema window must be > 0")
	}
	return &EMA{window: window, alpha: 2 / float64(window+1)}
}

// OnBar takes the bar's close and returns the EMA, and false until a full window
// has been seen to seed it.
func (e *EMA) OnBar(b marketdata.Bar) (float64, bool) {
	val := b.OHLC().Close.Float64()
	if e.count < e.window {
		e.count++
		e.sum += val
		if e.count < e.window {
			return 0, false
		}
		e.value = e.sum / float64(e.window)
		return e.value, true
	}
	e.value += e.alpha * (val - e.value)
	return e.value, true
}
