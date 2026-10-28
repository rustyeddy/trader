package main

import (
	"context"
	"fmt"
)

type strategy interface {
	init(cfg map[string]any) error
	onBar(ctx context.Context, bar ohlc) intent
}

type intent struct {
	kind string // open, close, noop
	side string // long or short
}

// emax is concrete implementation of emax
// will need to become an interface certainly
type emax struct {
	name  string
	short int
	long  int
	over  bool // is short over long

	emaShort *ema
	emaLong  *ema
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

func (e *emax) onBar(ctx context.Context, bar ohlc) intent {
	// Feed both EMAs every bar so the long one warms up on the same data.
	s, sok := e.emaShort.onBar(ctx, bar)
	l, lok := e.emaLong.onBar(ctx, bar)
	if !sok || !lok {
		return intent{side: "hold"}
	}

	switch {
	case s > l && !e.over:
		e.over = true
		return intent{side: "buy"}
	case s < l && e.over:
		e.over = false
		return intent{side: "sell"}
	}
	return intent{side: "hold"}
}
