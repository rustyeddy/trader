package main

import (
	"context"
	"fmt"
)

type strategy interface {
	name() string
	init(cfg map[string]any) error
	onBar(ctx context.Context, b bar) intent
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

func (e *emax) onBar(ctx context.Context, b bar) intent {
	// Feed both EMAs every bar so the long one warms up on the same data.
	s, sok := e.emaShort.onBar(ctx, b)
	l, lok := e.emaLong.onBar(ctx, b)

	// Every intent, including hold, names its instrument.
	noop := intent{instrument: b.instrument}
	if !sok || !lok {
		return noop
	}

	switch {
	case s > l && !e.over:
		e.over = true
		return intent{instrument: b.instrument, direction: long, action: open}
	case s < l && e.over:
		e.over = false
		return intent{instrument: b.instrument, action: close} // close takes no direction
	}
	return noop
}
