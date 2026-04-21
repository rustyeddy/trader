package backtest

import "github.com/rustyeddy/trader"

func BuildEMACrossConfig(r trader.ResolvedRun) (trader.EMACrossConfig, error) {
	return trader.BuildEMACrossConfigFromRun(r)
}
