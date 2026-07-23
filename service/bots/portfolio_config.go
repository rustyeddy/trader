package botsvc

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
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
	UseStream    bool    `yaml:"use_stream"`    // use OANDA pricing stream instead of polling
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
