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
// instrument (identity), marketdata (canonical bars), and indicator
// (ZScore), but must never import strategy, broker, execution, risk,
// pipeline, backtest, service, cmd, adapters, or journal — enforced
// mechanically by boundary_test.go. analysis produces statistical
// observations, never order intents: EventStudy reports what happened
// after a deviation, it does not decide whether to trade one.
//
// # Provenance
//
// Issue #280's own acceptance criteria requires a Result to carry
// enough provenance to reproduce the run: instrument, interval, span,
// and parameters. EventStudyConfig.Instrument and .Interval are
// required fields (validated, not merely documented), so a Result is
// self-describing about what it was computed from — and, since an
// hour-labeled Horizon like "4h" only means four hours at an actual
// hourly cadence, EventStudyConfig.validate cross-checks such a
// Horizon against Interval whenever Interval has a fixed,
// calendar-independent bar duration, catching a mislabeled-cadence run
// at construction rather than letting it silently mislead a reader of
// the results.
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
