// Package indicator owns mathematical transforms over price series, as
// scoped by issue #248 (EMA-03): only the streaming exponential moving
// average the EMA crossover milestone actually needs, not a general
// indicator framework.
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
// criterion).
package indicator
