package trader

import (
	"context"
	"fmt"
)

type strategy interface {
	name() string
	init() error
	onBar(ctx context.Context, b bar) *order
}

// emax is the EMA crossover strategy: long while the short EMA is above the long
// one, short while it is below.
type emax struct {
	id    string
	short int
	long  int

	// stopPct is how far from the entry price the stop is placed, as a
	// fraction of that price. Every open order carries the stop it produces.
	stopPct rate

	// signal state, not the actual position: the last entry signal consumed
	pos     direction // side it is in (noDirection: flat)
	pending direction // side to open on the next bar, after a flipping close

	emaShort *ema
	emaLong  *ema
}

func (e *emax) name() string {
	return e.id
}

func (e *emax) init() error {
	if e.short <= 0 || e.long <= 0 {
		return fmt.Errorf("emax: windows must be > 0 (short=%d long=%d)", e.short, e.long)
	}
	if e.short >= e.long {
		return fmt.Errorf("emax: short window (%d) must be less than long window (%d)", e.short, e.long)
	}
	if e.stopPct <= 0 || int64(e.stopPct) >= rateScale {
		return fmt.Errorf("emax: stop must be between 0%% and 100%% of price (stopPct=%d)", e.stopPct)
	}
	e.emaShort = newEMA(e.short)
	e.emaLong = newEMA(e.long)
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
func (e *emax) onBar(ctx context.Context, b bar) *order {
	// Feed both EMAs every bar so the long one warms up on the same data.
	s, sok := e.emaShort.onBar(b)
	l, lok := e.emaLong.onBar(b)
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
			instrument: b.instrument,
			price:      b.c,
			action:     close,
			orderType:  marketOrder,
			Time:       b.Time,
		}
	}
}

func (e *emax) openOrder(b bar, d direction) *order {
	stop := e.stopFor(b.c, d)
	if stop <= 0 || stop > maxPrice {
		return nil // skip the entry; clamping would change the configured risk
	}
	return &order{
		instrument: b.instrument,
		price:      b.c,
		stopLoss:   stop,
		action:     open,
		orderType:  marketOrder,
		direction:  d,
		Time:       b.Time,
	}
}

// stopFor places the stop stopPct of the entry price away on the losing side:
// below a long, above a short. The distance rounds down but is at least one
// price point.
func (e *emax) stopFor(entry price, d direction) price {
	dist := price(int64(entry) * int64(e.stopPct) / rateScale)
	if dist < 1 {
		dist = 1
	}
	if d == short {
		return entry + dist
	}
	return entry - dist
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
	stopPct     rate

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
		e := &emax{id: s.id + "/" + inst, short: s.short, long: s.long, stopPct: s.stopPct}
		if err := e.init(); err != nil {
			return fmt.Errorf("emax each %s: %w", inst, err)
		}
		s.byInstrument[inst] = e
	}
	return nil
}

func (s *emaxEach) onBar(ctx context.Context, b bar) *order {
	e, ok := s.byInstrument[b.instrument]
	if !ok {
		return nil
	}
	return e.onBar(ctx, b)
}
