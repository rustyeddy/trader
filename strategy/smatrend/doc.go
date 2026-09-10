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
// long-only: it never opens a short position. When the configured
// ExitRule manages a resting stop (the default, "trailing-stop" —
// see below; "sma-cross" places no resting stop at all and instead
// exits directly), that trigger detection is not smatrend's own
// responsibility: the resting order.IntentAdjustStop it places is
// triggered by the real broker-side machinery (ADR-026, wired into
// backtest.Scheduler by issue #338), so by the time OnBar observes a
// bar, any stop hit against that bar's own price action has already
// closed the position in the account snapshot smatrend reads through
// strategy.View.
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
//     flat, above the SMA, and after at least one prior exit, whether
//     to re-enter — gated centrally by Strategy itself (issue #349
//     review): while price remains below the SMA, only a fresh cross
//     re-enters, regardless of which ReEntryRule is configured, since
//     a rule governs *how* to resume within a still-bullish regime, not
//     whether to override it. The default, "fresh-cross", is
//     smatrend's own original behavior: require another fresh
//     cross-above, the same trigger the first entry uses.
//     "reclaim-exit-price" re-enters as soon as the close moves back
//     above the level the strategy exited at, without waiting for the
//     SMA to catch up. "breakout" re-enters as soon as the close
//     exceeds the highest High observed since the exit, independent
//     of the old exit level.
//   - InitialEntryRule (Config.InitialEntryModeName) decides, once
//     per bar while flat and before this strategy has ever held or
//     exited a position, whether to make its very first entry (issue
//     #349 review) — a separate decision from ReEntryRule, which only
//     ever governs an entry that follows a prior exit. The default,
//     "fresh-cross", is smatrend's own original startup behavior:
//     wait for a genuine cross above the SMA, even if a run or live
//     session starts already above it (crossState's own zero value
//     means the very first ready bar can never itself report a
//     cross). "above-sma" instead enters on the first bar the SMA is
//     ready and price is already above it, with no cross required —
//     intended for a long-hold strategy that should not wait
//     indefinitely for a pullback-and-recross merely because the run
//     happened to start mid-trend.
//
// A third built-in ExitRule, "probation-trend" (issue #349), governs
// the SMA Long Hold playbook's own three-state position lifecycle —
// FLAT, PROBATION, and TRENDING — exposed as Strategy.Phase rather
// than staying private to the rule (see phase.go): a freshly entered
// position starts in PROBATION, protected by a tight stop just below
// the SMA (Config.InitialStopBelowSMA) plus an independent SMA-cross
// override, until its gain from its own real average fill price
// reaches Config.TrailActivationGain, at which point it transitions
// to TRENDING and switches to the ordinary high-water-mark trailing
// stop (Config.TrailingStopPercent, the same field "trailing-stop"
// uses) — the SMA no longer has any exit power once TRENDING. A
// same-bar activation never retroactively changes that bar's own
// already-decided stop; the transition takes effect starting the next
// bar. Every re-entry (via ReEntryRule/InitialEntryRule) restarts a
// fresh PROBATION episode with no carried-over high-water mark or
// activation state from a prior episode — except the high-water mark
// itself is tracked continuously "since entry" within one episode, so
// a spike observed during PROBATION still governs the TRENDING stop
// after activation.
//
// New rules are a new small type plus one registry entry in
// exitrule.go/reentryrule.go/initialentryrule.go — never a change to
// Strategy's own control flow. See those files for the exact
// contracts and docs/research/eqs-01-baseline-sma-trend.org for the
// reference SPY configuration and backtest results (predating issue
// #347/#349's rule pluggability — that reference run used the default
// rules above, unchanged by this revision).
package smatrend
