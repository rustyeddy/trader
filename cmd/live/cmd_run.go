package live

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
	"github.com/rustyeddy/trader/strategies/pulse"
)

// liveRunConfig is the YAML schema for `trader live run`.
type liveRunConfig struct {
	Instrument     string        `yaml:"instrument"`
	Env            string        `yaml:"env"`            // "practice" or "live"
	TickInterval   string        `yaml:"tick_interval"`  // e.g. "60s", "5m"
	MaxPositions   int           `yaml:"max_positions"`
	RiskPct        float64       `yaml:"risk_pct"`
	MaxUnits       int64         `yaml:"max_units"`       // hard cap on position size in units
	MaxPositionUSD float64       `yaml:"max_position_usd"` // cap on notional value per trade
	Strategy       strategyBlock `yaml:"strategy"`
}

type strategyBlock struct {
	Kind   string         `yaml:"kind"`
	Params map[string]any `yaml:"params"`
}

func newRunCmd(rc *trader.RootConfig) *cobra.Command {
	var (
		configFile   string
		accountID    string
		token        string
		env          string
		instrument   string
		tickInterval string
		dryRun       bool
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a live strategy against a demo or live OANDA account",
		Long: `Run a configured live strategy, polling prices and placing/closing orders
on the specified OANDA environment (default: practice).

Example:
  trader live run --config testdata/configs/pulse-demo.yml
  trader live run --config testdata/configs/pulse-demo.yml --dry-run
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := notifyContext(cmd.Context())
			defer cancel()

			// Load config file if given.
			cfg := liveRunConfig{
				Env:          "practice",
				TickInterval: "60s",
				MaxPositions: 2,
				RiskPct:      0.1,
			}
			if configFile != "" {
				data, err := os.ReadFile(configFile)
				if err != nil {
					return fmt.Errorf("read config %q: %w", configFile, err)
				}
				if err := yaml.Unmarshal(data, &cfg); err != nil {
					return fmt.Errorf("parse config %q: %w", configFile, err)
				}
			}

			// CLI flags override config file.
			if env != "" {
				cfg.Env = env
			}
			if instrument != "" {
				cfg.Instrument = instrument
			}
			if tickInterval != "" {
				cfg.TickInterval = tickInterval
			}

			if cfg.Instrument == "" {
				return fmt.Errorf("instrument is required (set in config or --instrument)")
			}

			interval, err := time.ParseDuration(cfg.TickInterval)
			if err != nil {
				return fmt.Errorf("invalid tick_interval %q: %w", cfg.TickInterval, err)
			}

			// Build strategy.
			strategy, err := buildStrategy(cfg)
			if err != nil {
				return fmt.Errorf("build strategy: %w", err)
			}

			if dryRun {
				fmt.Printf("Dry-run: strategy=%s instrument=%s interval=%s max_positions=%d risk_pct=%.2f%%",
					strategy.Name(), cfg.Instrument, interval, cfg.MaxPositions, cfg.RiskPct)
				if cfg.MaxUnits > 0 {
					fmt.Printf(" max_units=%d", cfg.MaxUnits)
				}
				if cfg.MaxPositionUSD > 0 {
					fmt.Printf(" max_position_usd=%.2f", cfg.MaxPositionUSD)
				}
				fmt.Println()
				return nil
			}

			// Build service with OANDA.
			svc, err := service.New(service.Config{
				Env:       cfg.Env,
				Token:     token,
				AccountID: accountID,
			})
			if err != nil {
				return err
			}
			if err := svc.ResolveAccount(ctx); err != nil {
				var amb service.AmbiguousAccountError
				if errors.As(err, &amb) {
					fmt.Fprintln(os.Stderr, "Multiple accounts — specify one with --account-id:")
					for _, id := range amb.Accounts {
						fmt.Fprintf(os.Stderr, "  %s\n", id)
					}
				}
				return err
			}

			fmt.Printf("Starting live strategy: %s | %s | %s env | tick=%s\n",
				strategy.Name(), cfg.Instrument, cfg.Env, interval)
			fmt.Println("Press Ctrl-C to stop.")

			return svc.RunLiveStrategy(ctx, service.LiveRunConfig{
				Instrument:     cfg.Instrument,
				TickInterval:   interval,
				Strategy:       strategy,
				RiskPct:        cfg.RiskPct,
				MaxUnits:       cfg.MaxUnits,
				MaxPositionUSD: cfg.MaxPositionUSD,
			})
		},
	}

	cmd.Flags().StringVar(&configFile, "config", "", "Path to live run YAML config file")
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token")
	cmd.Flags().StringVar(&env, "env", "", "OANDA environment: practice|live (overrides config)")
	cmd.Flags().StringVar(&instrument, "instrument", "", "Instrument, e.g. EUR_USD (overrides config)")
	cmd.Flags().StringVar(&tickInterval, "tick-interval", "", "Poll interval, e.g. 30s, 5m (overrides config)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print resolved config and exit without trading")

	return cmd
}

// buildStrategy constructs a LiveStrategy from the config's strategy block.
func buildStrategy(cfg liveRunConfig) (trader.LiveStrategy, error) {
	kind := strings.ToLower(strings.TrimSpace(cfg.Strategy.Kind))
	if kind == "" {
		kind = "pulse"
	}
	switch kind {
	case "pulse":
		pcfg := pulse.DefaultConfig()
		pcfg.MaxPositions = cfg.MaxPositions
		pcfg.RiskPct = cfg.RiskPct

		// Overlay params from config file.
		p := cfg.Strategy.Params
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
			if s, ok := v.(string); ok {
				pcfg.Side = s
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
	default:
		return nil, fmt.Errorf("unknown strategy kind %q (supported: pulse)", kind)
	}
}

func toInt(v any, def int) int {
	switch x := v.(type) {
	case int:
		return x
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
