package marketdata

import "github.com/rustyeddy/trader/market"

// Migration shims: expose the unexported candle-set machinery to the root
// trader package (and its tests) across the new boundary while callers are
// migrated to marketdata directly. Remove once nothing outside this package
// needs them. See docs/pkg-migration.org.

type (
	CandleSet         = candleSet
	CandleSetIterator = candleSetIterator
)

func NewMonthlyCandleSet(inst string, tf market.Timeframe, monthStart market.Timestamp, scale market.Scale6, source string) (*CandleSet, error) {
	return newMonthlyCandleSet(inst, tf, monthStart, scale, source)
}
