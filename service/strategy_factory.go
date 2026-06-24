package service

import (
	"fmt"
	"math"
	"strings"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategies/pulse"
	"github.com/rustyeddy/trader/strategies/scalper"
	"github.com/rustyeddy/trader/strategies/stress"
	"github.com/rustyeddy/trader/strategy"
)

// StrategyConfig identifies a strategy kind and its parameters.
// Used by both BotConfig and the CLI live-run command.
type StrategyConfig struct {
	Kind        string                `json:"kind"              yaml:"kind"`
	Granularity string                `json:"granularity"       yaml:"granularity"` // candle-based strategies only
	Params      map[string]any        `json:"params"            yaml:"params"`
	Exit        strategy.ExitConfig   `json:"exit"              yaml:"exit"`
	Regime      strategy.RegimeConfig `json:"regime"            yaml:"regime"`
	// WarmupBars is the number of bars to fetch from OANDA to prime indicators.
	WarmupBars int `json:"warmup_bars"       yaml:"warmup_bars"`
	// LocalWarmupBars is the number of bars to read from local candle store before
	// the OANDA fetch. Use 500+ for long-period regime filters (atr-percentile etc).
	LocalWarmupBars int `json:"local_warmup_bars" yaml:"local_warmup_bars"`
}

// BuildLiveStrategy constructs a trader.LiveStrategy from a StrategyConfig.
// Candle-based strategies (scalper, stress) are wrapped in a CandleStrategyAdapter.
// instrument must be in OANDA format, e.g. "EUR_USD".
func (s *Service) BuildLiveStrategy(cfg StrategyConfig, instrument string) (trader.LiveStrategy, error) {
	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	if kind == "" {
		kind = "pulse"
	}
	p := cfg.Params
	if p == nil {
		p = map[string]any{}
	}

	switch kind {
	case "pulse":
		pcfg := pulse.DefaultConfig()
		if v, ok := p["trade_every"]; ok {
			pcfg.TradeEvery = toInt(v, pcfg.TradeEvery)
		}
		if v, ok := p["hold_bars"]; ok {
			pcfg.HoldBars = toInt(v, pcfg.HoldBars)
		}
		if v, ok := p["max_positions"]; ok {
			pcfg.MaxPositions = toInt(v, pcfg.MaxPositions)
		}
		if v, ok := p["side"]; ok {
			if sv, ok := v.(string); ok {
				pcfg.Side = sv
			}
		}
		if v, ok := p["stop_pips"]; ok {
			pcfg.StopPips = toFloat(v, pcfg.StopPips)
		}
		if v, ok := p["take_pips"]; ok {
			pcfg.TakePips = toFloat(v, pcfg.TakePips)
		}
		if v, ok := p["risk_pct"]; ok {
			pcfg.RiskPct = toFloat(v, pcfg.RiskPct)
		}
		return pulse.New(pcfg)

	case "scalper":
		fastPeriod := toInt(p["fast_period"], 3)
		slowPeriod := toInt(p["slow_period"], 8)
		warmupBars := toInt(p["warmup_bars"], 20)
		granularity := cfg.Granularity
		if granularity == "" {
			granularity = "M1"
		}
		st, err := scalper.New(scalper.Config{FastPeriod: fastPeriod, SlowPeriod: slowPeriod})
		if err != nil {
			return nil, err
		}
		return NewCandleStrategyAdapter(CandleAdapterConfig{
			Strategy:    st,
			Instrument:  instrument,
			Granularity: granularity,
			WarmupBars:  warmupBars,
			OANDA:       s.OANDA,
			AccountID:   s.AccountID,
			Service:     s,
		}), nil

	case "stress":
		tradeEvery := toInt(p["trade_every"], 1)
		stopPct := toFloat(p["stop_pct"], 0.002)
		side := ""
		if v, ok := p["side"]; ok {
			if sv, ok := v.(string); ok {
				side = sv
			}
		}
		warmupBars := toInt(p["warmup_bars"], 0)
		granularity := cfg.Granularity
		if granularity == "" {
			granularity = "M1"
		}
		st, err := stress.New(stress.Config{TradeEvery: tradeEvery, StopBps: int(math.Round(stopPct * 100)), Side: side})
		if err != nil {
			return nil, err
		}
		return NewCandleStrategyAdapter(CandleAdapterConfig{
			Strategy:    st,
			Instrument:  instrument,
			Granularity: granularity,
			WarmupBars:  warmupBars,
			OANDA:       s.OANDA,
			AccountID:   s.AccountID,
			Service:     s,
		}), nil

	default:
		// Fall back to the backtest strategy registry — any strategy registered
		// via strategy.MustRegisterStrategy (donchian-v2, donchian-v4, bb-fade, …)
		// can run live when wrapped in a CandleStrategyAdapter.
		backtestStrat, err := strategy.GetStrategy(strategy.StrategyConfig{Kind: kind, Params: p})
		if err != nil {
			return nil, fmt.Errorf("unknown strategy kind %q: %w", kind, err)
		}
		exit, err := strategy.GetExitStrategy(cfg.Exit, market.PriceScale)
		if err != nil {
			return nil, fmt.Errorf("exit strategy for %q: %w", kind, err)
		}
		regime, err := strategy.GetRegimeFilter(cfg.Regime, market.PriceScale)
		if err != nil {
			return nil, fmt.Errorf("regime filter for %q: %w", kind, err)
		}
		granularity := cfg.Granularity
		if granularity == "" {
			granularity = "D"
		}
		warmup := cfg.WarmupBars
		if warmup <= 0 {
			warmup = 100
		}
		return NewCandleStrategyAdapter(CandleAdapterConfig{
			Strategy:        backtestStrat,
			Exit:            exit,
			Regime:          regime,
			Instrument:      instrument,
			Granularity:     granularity,
			WarmupBars:      warmup,
			LocalWarmupBars: cfg.LocalWarmupBars,
			OANDA:           s.OANDA,
			AccountID:       s.AccountID,
			Service:         s,
			Log:             s.Log,
		}), nil
	}
}

func toInt(v any, def int) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return def
}

func toFloat(v any, def float64) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return def
}
