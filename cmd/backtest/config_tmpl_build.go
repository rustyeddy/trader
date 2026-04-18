package backtest

import (
	"fmt"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/strategies"
	"github.com/rustyeddy/trader/types"
)

func BuildTemplateStrategyConfig(r trader.ResolvedRun) (strategies.TemplateStrategyConfig, error) {
	lookback, ok, err := getRunIntParam(r.Strategy.Params, "lookback")
	if err != nil {
		return strategies.TemplateStrategyConfig{}, err
	}
	if !ok || lookback <= 0 {
		return strategies.TemplateStrategyConfig{}, fmt.Errorf("missing or invalid param %q", "lookback")
	}

	threshold, ok, err := getRunFloatParam(r.Strategy.Params, "threshold")
	if err != nil {
		return strategies.TemplateStrategyConfig{}, err
	}
	if !ok {
		return strategies.TemplateStrategyConfig{}, fmt.Errorf("missing param %q", "threshold")
	}

	scale := r.Scale
	if scale <= 0 {
		scale = types.PriceScale
	}

	return strategies.TemplateStrategyConfig{
		StrategyConfig: strategies.StrategyConfig{},
		Lookback:       lookback,
		Threshold:      threshold,
		Scale:          scale,
	}, nil
}
