package design

import "context"

type backtest struct {
	strategy
	prices *marketData
}

func (bt *backtest) Run(ctx context.Context, intentQ chan intent) (result, error) {
	for {
		if bar, ok := bt.prices.nextBar(); ok {
			intent := bt.strategy.onBar(ctx, bar)
			intentQ <- intent
		} else {
			return result{}, nil
		}
	}
	return result{}, nil
}

type result struct {
}

func (r *result) writeJournal() error {
	return nil
}
