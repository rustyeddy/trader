// Package indicators is Trader's streaming indicators: EMA, SMA, ATR, RSI,
// RollingStdDev and ZScore, concrete types with no common interface. Trader's own
// strategies, its scanners and studies, and external strategies (examples/emax)
// all use these, so they all compute the same numbers.
//
// Each is built with NewX(period) and advanced one sample at a time with Update;
// Ready says whether it has seen enough samples, and Value is meaningful only once
// it has. Update rejects a NaN or infinite sample with ErrNonFiniteSample and
// leaves the indicator unchanged. Replaying the same samples through a fresh
// indicator gives bit-identical values.
//
// The samples are float64, analytical values (num.Price.Float64 converts a price,
// ADR-045). ATR is the exception: it takes num.Price and keeps its true ranges
// exact (see ATR).
//
// EMA is seeded with the simple average of its first Period samples and then
// follows value += alpha*(sample-value), alpha = 2/(Period+1). ATR is the simple
// average of the last Period true ranges, not Wilder-smoothed; RSI is Wilder's.
// SMA, RollingStdDev (population) and ZScore share one rolling window that
// includes the sample just supplied; ZScore has no value when the window has zero
// variance.
//
// The package knows nothing of strategies, brokers, accounts or services, and a
// test keeps it so. It is not a framework: there is no registry or factory, and a
// new indicator is a new type here when a strategy needs it.
package indicators
