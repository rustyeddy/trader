// Package emacross implements Trader's first real research strategy
// (issue #249, EMA-04): a fast/slow EMA crossover strategy against the
// public strategy.Strategy contract, exactly as specified by
// docs/research/ema-01-experiment-definition.org.
//
// emacross owns its own indicator.EMA state, declares its H1 data
// requirement (with WarmupBars equal to the slow period, per the
// experiment definition's Decision 2), and emits order.Intent values
// through the strategy.IntentFactory its Environment provides. It
// never imports broker, execution, risk, pipeline, backtest, service,
// cmd, or adapters — enforced mechanically by boundary_test.go, the
// same guard strategy/boundary_test.go and indicator/boundary_test.go
// already carry — and it decides no more than the experiment
// definition already settled: crossover semantics (crossstate.go),
// warm-up/readiness (delegated entirely to indicator.EMA), and
// flat/long/short transitions (strategy.go).
package emacross
