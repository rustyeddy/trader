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
	over  bool // is short over long

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
func (e *emax) onBar(ctx context.Context, b bar) (o *order) {
	// Feed both EMAs every bar so the long one warms up on the same data.
	s, sok := e.emaShort.onBar(ctx, b)
	l, lok := e.emaLong.onBar(ctx, b)
	if !sok || !lok {
		return nil
	}

	switch {
	case s > l && !e.over:
		e.over = true
		return &order{instrument: b.instrument, price: b.c, action: open, orderType: marketOrder, direction: long}
	case s < l && e.over:
		e.over = false
		// close takes no direction: it acts on the existing position
		return &order{instrument: b.instrument, price: b.c, action: close, orderType: marketOrder}
	}

	return nil
}
