package trader

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/indicators"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// strategy decides what to trade. It is called once per availability instant with
// every bar that became available then (the bars are fresh and in instrument
// order; an instrument with no bar at that instant is absent) and returns the
// orders it wants, in the order they should be submitted, or none, or an error,
// which stops the run before any order is used. It sees the
// whole group before any of its orders is submitted. A single-instrument run is
// the same call with a one-bar group. bars is only valid for the call.
type strategy interface {
	name() string
	init() error
	onBars(ctx context.Context, bars []marketdata.Bar) ([]*order, error)
}

// warmer is implemented by a strategy that can prefill its indicators. The bars
// it is given came before the first one the strategy trades: it learns from them
// (feeds its indicators) but takes no position and returns no order, so a run that
// starts at a date does not start its indicators cold. A strategy that does not
// implement it is simply not warmed. An error (a worker that cannot be reached, or
// answers a warm-up with an order) stops the run.
type warmer interface {
	warm(ctx context.Context, bars []marketdata.Bar) error
}

// eachBar adapts a decision made one bar at a time: it asks decide about every bar
// of the group in order and returns the orders it gave, in that order. A decision
// that is only a bar-by-bar rule cannot fail, so the error is always nil.
func eachBar(ctx context.Context, bars []marketdata.Bar, decide func(context.Context, marketdata.Bar) *order) ([]*order, error) {
	var orders []*order
	for _, b := range bars {
		if o := decide(ctx, b); o != nil {
			orders = append(orders, o)
		}
	}
	return orders, nil
}

// emax is the EMA crossover strategy: long while the short EMA is above the long
// one, short while it is below.
type emax struct {
	id    string
	short int
	long  int

	// stopPct is how far from the entry price the stop is placed, as a
	// fraction of that price. Every open order carries the stop it produces.
	stopPct num.Rate
	tick    num.Price // the instrument's price tick, from its Spec

	// signal state, not the actual position: the last entry signal consumed
	pos     direction // side it is in (noDirection: flat)
	pending direction // side to open on the next bar, after a flipping close

	emaShort *indicators.EMA
	emaLong  *indicators.EMA
}

func (e *emax) name() string {
	return e.id
}

// warm feeds both EMAs with the bar and nothing else: no signal state changes and
// no order comes of it. The first bar after the warm-up then finds the EMAs seeded,
// so EMAX sees the trend as it already stands, as it does at the first bar of any
// run once its EMAs are ready.
func (e *emax) warm(_ context.Context, bars []marketdata.Bar) error {
	for _, b := range bars {
		emaOnBar(e.emaShort, b)
		emaOnBar(e.emaLong, b)
	}
	return nil
}

// onBars is onBar for each bar of the group.
func (e *emax) onBars(ctx context.Context, bars []marketdata.Bar) ([]*order, error) {
	return eachBar(ctx, bars, e.onBar)
}

func (e *emax) init() error {
	if e.short <= 0 || e.long <= 0 {
		return fmt.Errorf("emax: windows must be > 0 (short=%d long=%d)", e.short, e.long)
	}
	if e.short >= e.long {
		return fmt.Errorf("emax: short window (%d) must be less than long window (%d)", e.short, e.long)
	}
	if e.stopPct.Sign() <= 0 || e.stopPct.Cmp(rateOne) >= 0 {
		return fmt.Errorf("emax: stop must be between 0%% and 100%% of price (stopPct=%s)", e.stopPct)
	}
	if e.tick.IsZero() {
		return fmt.Errorf("emax: no price tick")
	}
	var err error
	if e.emaShort, err = indicators.NewEMA(e.short); err != nil {
		return fmt.Errorf("emax: short window: %w", err)
	}
	if e.emaLong, err = indicators.NewEMA(e.long); err != nil {
		return fmt.Errorf("emax: long window: %w", err)
	}
	return nil
}

// onBar returns the order the strategy wants for this bar, or nil when there
// is nothing to do.
//
// The strategy signals long when the short EMA is above the long EMA and
// short when it is below. A flip is a close on the crossover bar followed by
// the opposite open on the next bar: an onBar returns one order, and there is
// no atomic reverse. An open that is rejected is not retried; the next order
// comes only from the next crossover. An entry whose stop falls outside the
// supported price range is skipped, with the same no-retry behavior.
//
// The signal state is the last entry signal consumed, not the position. A
// broker stop-out does not change it, so after one the strategy stays flat
// until the trend turns, then emits a close the broker rejects with
// errNoPosition, then the opposite open. This is the current contract, not an
// oversight; see rule 11 in emax-validation.org and TODO 1 in todo.org.
func (e *emax) onBar(ctx context.Context, b marketdata.Bar) *order {
	// Feed both EMAs every bar so the long one warms up on the same data.
	s, sok := emaOnBar(e.emaShort, b)
	l, lok := emaOnBar(e.emaLong, b)
	if !sok || !lok {
		return nil
	}

	// Finish a flip: the close went out last bar, now open the other side.
	if e.pending != noDirection {
		d := e.pending
		e.pending = noDirection
		e.pos = d
		return e.openOrder(b, d)
	}

	var trend direction
	switch {
	case s > l:
		trend = long
	case s < l:
		trend = short
	default:
		return nil // equal: no change
	}

	switch {
	case trend == e.pos:
		return nil
	case e.pos == noDirection:
		e.pos = trend
		return e.openOrder(b, trend)
	default:
		// in the opposite side: close now, open the new side next bar
		e.pos = noDirection
		e.pending = trend
		// close takes no direction: it acts on the existing position
		return &order{
			instrument: b.Instrument,
			price:      b.OHLC().Close,
			action:     close,
			orderType:  marketOrder,
			Time:       b.Time,
		}
	}
}

func (e *emax) openOrder(b marketdata.Bar, d direction) *order {
	stop, err := e.stopFor(b.OHLC().Close, d)
	if err != nil || stop.IsZero() || stop.Cmp(maxPrice) > 0 {
		return nil // skip the entry; clamping would change the configured risk
	}
	return &order{
		instrument: b.Instrument,
		price:      b.OHLC().Close,
		stopLoss:   stop,
		action:     open,
		orderType:  marketOrder,
		direction:  d,
		Time:       b.Time,
	}
}

// stopFor places the stop stopPct of the entry price away on the losing side:
// below a long, above a short. The distance rounds down to the price tick but
// is at least one tick. A stop that would fall below zero is an error, and the
// entry is skipped.
//
// The product is rounded half-even at 1e-8 before the floor, so a product just
// below a tick boundary can land on it; the stop can be at most one tick wider
// than the exact floor (ADR-087). The distance is not a safety quantity: sizing
// uses the actual distance to the stop.
func (e *emax) stopFor(entry num.Price, d direction) (num.Price, error) {
	raw, err := entry.MulRate(e.stopPct)
	if err != nil {
		return num.Price{}, err
	}
	dist, err := raw.RoundDown(e.tick)
	if err != nil {
		return num.Price{}, err
	}
	if dist.IsZero() {
		dist = e.tick
	}
	if d == short {
		return entry.Add(dist)
	}
	return entry.Sub(dist)
}

// emaOnBar feeds an EMA the bar's close and returns its value, and false until it
// is seeded. A close is a price, so it is always finite and Update cannot refuse it.
func emaOnBar(e *indicators.EMA, b marketdata.Bar) (float64, bool) {
	if err := e.Update(b.OHLC().Close.Float64()); err != nil {
		panic("emax: " + err.Error())
	}
	return e.Value(), e.Ready()
}

// emaxEach trades EMAX on each of several instruments through one trader,
// with separate EMA and signal state per instrument: one emax for each, built
// from the same settings. A bar is handed to its instrument's emax alone, so
// it returns at most one order, and a frame may produce one per instrument.
// Bars of instruments it was not given are not traded.
type emaxEach struct {
	id          string
	instruments []string
	short, long int
	stopPct     num.Rate
	markets     *catalog // supplies each instrument's tick

	byInstrument map[string]*emax
}

func (s *emaxEach) name() string {
	return s.id
}

func (s *emaxEach) init() error {
	if len(s.instruments) == 0 {
		return fmt.Errorf("emax each: no instruments")
	}
	s.byInstrument = make(map[string]*emax, len(s.instruments))
	for _, inst := range s.instruments {
		if _, dup := s.byInstrument[inst]; dup {
			return fmt.Errorf("emax each: instrument %q given twice", inst)
		}
		m, ok := s.markets.lookup(inst)
		if !ok {
			return fmt.Errorf("emax each: %w: %q", errUnknownInstrument, inst)
		}
		e := &emax{id: s.id + "/" + inst, short: s.short, long: s.long, stopPct: s.stopPct, tick: m.tick}
		if err := e.init(); err != nil {
			return fmt.Errorf("emax each %s: %w", inst, err)
		}
		s.byInstrument[inst] = e
	}
	return nil
}

// warm hands each bar to its instrument's emax; bars of instruments it was not
// given are ignored.
func (s *emaxEach) warm(ctx context.Context, bars []marketdata.Bar) error {
	for _, b := range bars {
		if e, ok := s.byInstrument[b.Instrument]; ok {
			if err := e.warm(ctx, []marketdata.Bar{b}); err != nil {
				return err
			}
		}
	}
	return nil
}

// onBars hands each bar to its instrument's emax; the group's orders come back in
// the group's instrument order.
func (s *emaxEach) onBars(ctx context.Context, bars []marketdata.Bar) ([]*order, error) {
	return eachBar(ctx, bars, s.onBar)
}

func (s *emaxEach) onBar(ctx context.Context, b marketdata.Bar) *order {
	e, ok := s.byInstrument[b.Instrument]
	if !ok {
		return nil
	}
	return e.onBar(ctx, b)
}
