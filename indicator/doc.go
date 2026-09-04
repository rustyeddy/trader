// Package indicator owns mathematical transforms over price series.
// Issue #248 (EMA-03) scoped the initial package to only the streaming
// exponential moving average the EMA crossover milestone needed, not a
// general indicator framework; issue #279 (MR-02) added SMA,
// RollingStdDev, and ZScore as the analytical primitives
// docs/research/mr-01-experiment-definition.org needs to construct its
// rolling mean/standard-deviation/normalized-deviation observation.
// Each addition remains scoped to what a concrete milestone actually
// needs — this is still not a general indicator framework.
//
// # Architectural boundary
//
// indicator is broker/backtest-independent (architecture document's own
// "Indicators calculate values; strategies interpret those values"
// principle). It must never import strategy, backtest, service,
// execution, risk, broker, cmd, or adapters — enforced mechanically by
// boundary_test.go — and it never decides whether to buy or sell: an
// EMA reports EMA values, nothing more.
//
// # Numeric representation and rounding policy
//
// Indicator arithmetic uses float64 throughout, per the architecture
// document's own guidance ("indicators may use floating point
// internally"; exact fixed-point types are reserved for prices,
// quantities, and money at the order/accounting boundary, not for
// analytical calculations). No intermediate rounding is applied: each
// EMA update accumulates the standard recurrence
// (EMA_t = sample*k + EMA_{t-1}*(1-k)) directly in IEEE 754 double
// precision. A caller converting a canonical num.Price close into the
// float64 sample EMA consumes is performing an analytical conversion,
// not an accounting one, and is responsible for that conversion itself
// — indicator never imports num. That conversion is num.Price's own
// Float64 method (ADR-045,
// docs/arch/adr-045-analytical-float64-conversion-boundary.org): a
// direct numeric conversion, never a String()/strconv.ParseFloat()
// round-trip through decimal text.
//
// This is deterministic: Go's float64 arithmetic is fully specified and
// reproducible for a given, fixed sequence of operations, so replaying
// an identical input sequence through a freshly constructed EMA always
// produces bit-identical output (issue #248's own acceptance
// criterion). SMA, RollingStdDev, and ZScore share this same
// determinism property, verified by their own replay tests.
//
// # SMA, RollingStdDev, and ZScore rolling-window semantics
//
// SMA, RollingStdDev, and ZScore are all built on one shared, unexported
// sliding-window accumulator (window.go) that answers a question
// docs/research/mr-01-experiment-definition.org explicitly left open:
// the window ending at time t is inclusive of the sample supplied at t
// itself, matching the conventional SMA/Bollinger-Band definition. The
// accumulator tracks mean and variance via Welford's online algorithm,
// adapted to support removing the oldest sample as well as adding a new
// one, which stays numerically stable for FX-close-price-shaped input
// (a large common magnitude with a small spread) where a naive
// sum/sum-of-squares approach would lose precision to cancellation.
// Variance is population variance (divide by the window's own sample
// count, not n-1), the conventional choice for a rolling/Bollinger-style
// indicator describing the dispersion of the window's own contents.
//
// ZScore.Value reports (0, false) rather than a NaN or infinite score
// when the rolling window has zero variance (every sample in the window
// identical), per docs/research/mr-01-experiment-definition.org's
// explicit requirement that such an observation be excluded rather than
// treated as a real (if extreme) value.
package indicator
