// Package smatrend implements Trader's first baseline long-term equity
// trend-following strategy (issue #335, EQS-01): be long only above a
// simple moving average, protected by a monotonic trailing stop.
//
// smatrend owns its own indicator.SMA state, declares a data
// requirement for whatever instrument/interval New was given (D1 for
// the reference SPY strategy, but New does not itself enforce that —
// WarmupBars equals the SMA period regardless), and emits order.Intent
// values through the strategy.IntentFactory its Environment provides.
// It never imports broker, execution, risk, pipeline, backtest,
// service, cmd, or adapters — enforced mechanically by
// boundary_test.go, the same guard strategy/emacross/boundary_test.go
// already carries.
//
// Unlike strategy/emacross, smatrend is deliberately asymmetric and
// long-only: it never opens a short position, and it exits a position
// only through its own trailing stop — never because price falls back
// below the SMA. Its own trailing-stop trigger detection is not
// smatrend's own responsibility at all: the resting
// order.IntentAdjustStop it places is triggered by the real
// broker-side machinery (ADR-026, wired into backtest.Scheduler by
// issue #338), so by the time OnBar observes a bar, any stop hit
// against that bar's own price action has already closed the position
// in the account snapshot smatrend reads through strategy.View.
// smatrend's own job is only: track the high-water mark and the
// monotonic (never-decreasing) stop level while long, and require a
// fresh SMA cross-above (not merely "still above the SMA") before
// re-entering after a stop exit.
package smatrend
