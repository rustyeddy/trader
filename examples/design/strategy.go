package main

import (
	"context"
	"fmt"
)

type strategy interface {
	name() string
	init(cfg map[string]any) error
	onBar(ctx context.Context, b bar) *order
}

// emax is concrete implementation of emax
// will need to become an interface certainly
type emax struct {
	id    string
	short int
	long  int

	// signal state, not the actual position: what the strategy last asked for
	pos     direction // side it is in (noDirection: flat)
	pending direction // side to open on the next bar, after a flipping close

	emaShort *ema
	emaLong  *ema
}

func (e *emax) name() string {
	return e.id
}

func (e *emax) init(cfg map[string]any) error {
	if e.short <= 0 || e.long <= 0 {
		return fmt.Errorf("emax: windows must be > 0 (short=%d long=%d)", e.short, e.long)
	}
	if e.short >= e.long {
		return fmt.Errorf("emax: short window (%d) must be less than long window (%d)", e.short, e.long)
	}
	e.emaShort = newEMA(e.short)
	e.emaLong = newEMA(e.long)
	return nil
}

// onBar returns the order the strategy wants for this bar, or nil when there
// is nothing to do.
//
// The strategy is always long when the short EMA is above the long EMA and
// short when it is below. A flip is a close on the crossover bar followed by
// the opposite open on the next bar: an onBar returns one order, and there is
// no atomic reverse. An open that is rejected is not retried; the next order
// comes only from the next crossover.
func (e *emax) onBar(ctx context.Context, b bar) *order {
	// Feed both EMAs every bar so the long one warms up on the same data.
	s, sok := e.emaShort.onBar(ctx, b)
	l, lok := e.emaLong.onBar(ctx, b)
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
		return &order{instrument: b.instrument, price: b.c, action: close, orderType: marketOrder}
	}
}

func (e *emax) openOrder(b bar, d direction) *order {
	return &order{instrument: b.instrument, price: b.c, action: open, orderType: marketOrder, direction: d}
}
