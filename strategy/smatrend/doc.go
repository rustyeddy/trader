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
// long-only: it never opens a short position. Its own trailing-stop
// trigger detection is not smatrend's own responsibility at all: the
// resting order.IntentAdjustStop it places is triggered by the real
// broker-side machinery (ADR-026, wired into backtest.Scheduler by
// issue #338), so by the time OnBar observes a bar, any stop hit
// against that bar's own price action has already closed the position
// in the account snapshot smatrend reads through strategy.View.
//
// Both what closes a long position and what re-opens one after an
// exit are pluggable (issue #347), each behind a small interface
// selected by name in Config:
//
//   - ExitRule (Config.ExitRuleName) decides, once per bar while
//     long, whether to exit now or move the resting stop. The default,
//     "trailing-stop", is smatrend's own original behavior: track the
//     high-water mark and ratchet a monotonic (never-decreasing) stop
//     level, never exiting because price merely falls back below the
//     SMA. "sma-cross" is the one other built-in: exit outright once
//     the close is at or below the SMA, with no resting stop at all.
//   - ReEntryRule (Config.ReEntryRuleName) decides, once per bar while
//     flat and after at least one prior exit, whether to re-enter. The
//     very first entry ever always requires a fresh SMA cross-above,
//     regardless of which ReEntryRule is configured — there is
//     nothing yet to "reclaim" or "break out of." The default,
//     "fresh-cross", is smatrend's own original behavior: require
//     another fresh cross-above, the same trigger the first entry
//     uses. "reclaim-exit-price" re-enters as soon as the close moves
//     back above the level the strategy exited at, without waiting
//     for the SMA to catch up. "breakout" re-enters as soon as the
//     close exceeds the highest High observed since the exit,
//     independent of the old exit level.
//
// New rules are a new small type plus one registry entry in
// exitrule.go/reentryrule.go — never a change to Strategy's own
// control flow. See exitrule.go and reentryrule.go for the exact
// contracts and docs/research/eqs-01-baseline-sma-trend.org for the
// reference SPY configuration and backtest results (predating issue
// #347's rule pluggability — that reference run used the default
// rules above, unchanged by this revision).
package smatrend
