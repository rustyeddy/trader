package main

import "context"

type backtest struct {
}

func (bt *backtest) run(ctx context.Context, priceQ <-chan ohlc, strat strategy, tr *trader) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case bar, ok := <-priceQ:
			if !ok {
				return nil
			}
			intent := strat.onBar(ctx, bar)
			tr.handleIntent(ctx, intent)
		}
	}
	return nil
}

type result struct {
}

func (r *result) writeJournal() error {
	return nil
}
