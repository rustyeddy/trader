package trader

import "context"

// Strategy is the single backtest strategy interface used across the repo.
type Strategy interface {
	Name() string
	Reset()
	Ready() bool
	Update(context.Context, *CandleTime, *BacktestRun) *StrategyPlan
}

type strategyRuntimeKey struct{}

type strategyRuntime struct {
	instrument string
	barIndex   int
	gapBars    int
	account    *Account
}

func withStrategyRuntime(ctx context.Context, instrument string, barIndex, gapBars int, account *Account) context.Context {
	return context.WithValue(ctx, strategyRuntimeKey{}, strategyRuntime{
		instrument: instrument,
		barIndex:   barIndex,
		gapBars:    gapBars,
		account:    account,
	})
}

func strategyRuntimeFromContext(ctx context.Context) (strategyRuntime, bool) {
	rt, ok := ctx.Value(strategyRuntimeKey{}).(strategyRuntime)
	return rt, ok
}

func StrategyInstrument(ctx context.Context) string {
	rt, ok := strategyRuntimeFromContext(ctx)
	if !ok {
		return ""
	}
	return rt.instrument
}

func StrategyBarIndex(ctx context.Context) int {
	rt, ok := strategyRuntimeFromContext(ctx)
	if !ok {
		return 0
	}
	return rt.barIndex
}

func StrategyGapBars(ctx context.Context) int {
	rt, ok := strategyRuntimeFromContext(ctx)
	if !ok {
		return 0
	}
	return rt.gapBars
}

func StrategyAccount(ctx context.Context) *Account {
	rt, ok := strategyRuntimeFromContext(ctx)
	if !ok {
		return nil
	}
	return rt.account
}

type StrategyBaseConfig struct {
	Instrument string
}
