package backtest

import (
	"fmt"

	"github.com/rustyeddy/trader"
)

func BuildTemplateStrategyConfig(r trader.ResolvedRun) (trader.TemplateStrategyConfig, error) {
	lookback, ok, err := getRunIntParam(r.Strategy.Params, "lookback")
	if err != nil {
		return trader.TemplateStrategyConfig{}, err
	}
	if !ok || lookback <= 0 {
		return trader.TemplateStrategyConfig{}, fmt.Errorf("missing or invalid param %q", "lookback")
	}

	threshold, ok, err := getRunFloatParam(r.Strategy.Params, "threshold")
	if err != nil {
		return trader.TemplateStrategyConfig{}, err
	}
	if !ok {
		return trader.TemplateStrategyConfig{}, fmt.Errorf("missing param %q", "threshold")
	}

	scale := r.Scale
	if scale <= 0 {
		scale = trader.PriceScale
	}

	return trader.TemplateStrategyConfig{
		StrategyBaseConfig: trader.StrategyBaseConfig{},
		Lookback:           lookback,
		Threshold:          threshold,
		Scale:              scale,
	}, nil
}
