package backtest

import "github.com/rustyeddy/trader"

func BuildTemplateStrategyConfig(r trader.ResolvedRun) (trader.TemplateStrategyConfig, error) {
	return trader.BuildTemplateStrategyConfigFromRun(r)
}
