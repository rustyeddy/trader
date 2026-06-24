package service

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
)

// PortfolioConfig is the YAML schema for `trader live portfolio`.
type PortfolioConfig struct {
	Env                string  `yaml:"env"`                  // "practice" or "live"
	AccountID          string  `yaml:"account_id"`           // OANDA account ID (overrides --account-id flag and env var)
	RiskPct            float64 `yaml:"risk_pct"`             // default risk per trade (%)
	DrawdownCircuitPct float64 `yaml:"drawdown_circuit_pct"` // halt opens when equity drops this % from peak
	// LocalWarmupBars is the default number of bars to load from the local
	// candle store for indicator priming. Per-instrument values override this.
	// 500 covers ~3 weeks of H1 data; 5000 covers ~7 months. 0 disables.
	LocalWarmupBars int                       `yaml:"local_warmup_bars"`
	Instruments     []portfolioInstrumentYAML `yaml:"instruments"`
}

type portfolioInstrumentYAML struct {
	Instrument   string  `yaml:"instrument"`    // OANDA format, e.g. "USD_CHF"
	Timeframe    string  `yaml:"timeframe"`     // "H1" or "D1"
	TickInterval string  `yaml:"tick_interval"` // optional poll override, e.g. "5m"
	RiskPct      float64 `yaml:"risk_pct"`      // overrides top-level default
	MaxUnits     int64   `yaml:"max_units"`
	WarmupBars   int     `yaml:"warmup_bars"`
	// LocalWarmupBars overrides the portfolio-level default for this instrument.
	LocalWarmupBars int `yaml:"local_warmup_bars"`

	Strategy struct {
		Kind   string         `yaml:"kind"`
		Params map[string]any `yaml:"params"`
	} `yaml:"strategy"`

	Exit struct {
		Kind   string         `yaml:"kind"`
		Params map[string]any `yaml:"params"`
	} `yaml:"exit"`

	Regime struct {
		Kind    string         `yaml:"kind"`
		Params  map[string]any `yaml:"params"`
		Filters []struct {
			Kind   string         `yaml:"kind"`
			Params map[string]any `yaml:"params"`
		} `yaml:"filters"`
	} `yaml:"regime"`
}

// LoadPortfolioConfig reads and parses a portfolio YAML file.
func LoadPortfolioConfig(path string) (*PortfolioConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read portfolio config %q: %w", path, err)
	}
	cfg := &PortfolioConfig{
		Env:                "practice",
		RiskPct:            1.0,
		DrawdownCircuitPct: 10.0,
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse portfolio config %q: %w", path, err)
	}
	return cfg, nil
}

// BuildPortfolioRunConfig wires up the full PortfolioRunConfig from a parsed
// PortfolioConfig, building CandleStrategyAdapters for every instrument.
func BuildPortfolioRunConfig(cfg *PortfolioConfig, oandaClient *oanda.Client, accountID string, log *slog.Logger) (*PortfolioRunConfig, error) {
	rc := &PortfolioRunConfig{
		DrawdownCircuitPct: cfg.DrawdownCircuitPct,
		Log:                log,
	}

	for _, y := range cfg.Instruments {
		scfg := strategy.StrategyConfig{Kind: y.Strategy.Kind, Params: y.Strategy.Params}

		riskPct := y.RiskPct
		if riskPct <= 0 {
			riskPct = cfg.RiskPct
		}

		// Native LiveStrategy (e.g. pulse) — bypasses candle adapter entirely.
		if trader.LookupLiveStrategy(scfg.Kind) != nil {
			liveStrat, err := trader.GetLiveStrategy(scfg)
			if err != nil {
				return nil, fmt.Errorf("instrument %s strategy: %w", y.Instrument, err)
			}
			rc.Instruments = append(rc.Instruments, InstrumentRunConfig{
				Instrument:  y.Instrument,
				Granularity: toOandaGranularity(y.Timeframe),
				TickInterval: func() time.Duration {
					d, _ := time.ParseDuration(y.TickInterval)
					return d
				}(),
				Strategy: liveStrat,
				RiskPct:  riskPct,
				MaxUnits: y.MaxUnits,
			})
			continue
		}

		// Backtest strategy — wrap in candle adapter for bar-driven live trading.
		strat, err := strategy.GetStrategy(scfg)
		if err != nil {
			return nil, fmt.Errorf("instrument %s strategy: %w", y.Instrument, err)
		}

		regimeCfg := strategy.RegimeConfig{Kind: y.Regime.Kind, Params: y.Regime.Params}
		for _, f := range y.Regime.Filters {
			regimeCfg.Filters = append(regimeCfg.Filters, strategy.RegimeConfig{Kind: f.Kind, Params: f.Params})
		}
		regime, err := strategy.GetRegimeFilter(regimeCfg, market.PriceScale)
		if err != nil {
			return nil, fmt.Errorf("instrument %s regime: %w", y.Instrument, err)
		}

		granularity := toOandaGranularity(y.Timeframe)
		warmup := y.WarmupBars
		if warmup <= 0 {
			warmup = 100
		}
		localWarmup := y.LocalWarmupBars
		if localWarmup <= 0 {
			localWarmup = cfg.LocalWarmupBars
		}

		exitCfg := strategy.ExitConfig{Kind: y.Exit.Kind, Params: y.Exit.Params}
		exit, err := strategy.GetExitStrategy(exitCfg, market.PriceScale)
		if err != nil {
			return nil, fmt.Errorf("instrument %s exit: %w", y.Instrument, err)
		}

		// Wrap the service so the adapter can update trailing stops on OANDA.
		svc := &Service{OANDA: oandaClient, AccountID: accountID, Log: log}
		adapter := NewCandleStrategyAdapter(CandleAdapterConfig{
			Strategy:        strat,
			Exit:            exit,
			Regime:          regime,
			Instrument:      y.Instrument,
			Granularity:     granularity,
			WarmupBars:      warmup,
			LocalWarmupBars: localWarmup,
			OANDA:           oandaClient,
			AccountID:       accountID,
			Service:         svc,
			Log:             log,
		})

		var tick time.Duration
		if y.TickInterval != "" {
			tick, err = time.ParseDuration(y.TickInterval)
			if err != nil {
				return nil, fmt.Errorf("instrument %s tick_interval: %w", y.Instrument, err)
			}
		}

		rc.Instruments = append(rc.Instruments, InstrumentRunConfig{
			Instrument:   y.Instrument,
			Granularity:  granularity,
			TickInterval: tick,
			Strategy:     adapter,
			RiskPct:      riskPct,
			MaxUnits:     y.MaxUnits,
		})
	}
	return rc, nil
}
