package main

import "context"

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
}

func (e *emax) init(cfg map[string]any) error {
	return nil
}

func (e *emax) onBar(ctx context.Context, bar ohlc) (i intent) {
	return i
}
