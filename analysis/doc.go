// Package analysis owns observations derived from prices and
// indicators that are not themselves orders or broker actions
// (architecture document's "analysis" package responsibility:
// trend/regime classification, volatility estimates, statistical
// features, and similar derived observations).
//
// Issue #280 (MR-03) scopes the initial package to exactly one
// capability: a deterministic Z-score forward-return event study, the
// analyzer docs/research/mr-01-experiment-definition.org needs to
// measure whether large normalized price deviations tend to revert.
// This is not a general analysis framework — like indicator before it,
// analysis grows one concrete, milestone-scoped capability at a time.
//
// # Architectural boundary
//
// analysis sits between marketdata/indicator and strategy in the
// architecture document's dependency graph: it may depend on
// marketdata (canonical bars) and indicator (ZScore), but must never
// import strategy, broker, execution, risk, pipeline, backtest,
// service, cmd, adapters, or journal — enforced mechanically by
// boundary_test.go. analysis produces statistical observations, never
// order intents: EventStudy reports what happened after a deviation,
// it does not decide whether to trade one.
//
// # No lookahead
//
// RunEventStudy constructs each Observation at bar index i using only
// bars[0:i+1] — the same Welford-based indicator.ZScore accumulator
// MR-02 built, fed one bar at a time in order. Bars after i are used
// only to label that observation's forward-return outcome at each
// configured Horizon, never to influence the observation itself. This
// is verified directly by TestRunEventStudy_ObservationsAreLookaheadFree,
// which truncates the input bar slice and confirms every Observation
// up to the truncation point is unchanged by what the caller does not
// yet supply.
//
// # Numeric representation
//
// Like indicator, analysis computes entirely in float64: Z-scores and
// forward returns are analytical statistics, not accounting values
// (architecture document's own "analytical calculations may use
// floating point" guidance). The one conversion from an exact
// num.Price close into the float64 domain RunEventStudy needs happens
// through Price's own sanctioned Float64 method (ADR-045), never a
// String()/ParseFloat() round-trip.
package analysis
