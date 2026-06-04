package live

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
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
	Kind        string         `yaml:"kind"`
	Granularity string         `yaml:"granularity"` // candle bar size for candle-based strategies, e.g. "M1", "H1"
	Params      map[string]any `yaml:"params"`
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

			if dryRun {
				kind := cfg.Strategy.Kind
				if kind == "" {
					kind = "pulse"
				}
				fmt.Printf("Dry-run: kind=%s instrument=%s interval=%s max_positions=%d risk_pct=%.2f%%",
					kind, cfg.Instrument, interval, cfg.MaxPositions, cfg.RiskPct)
				if cfg.MaxUnits > 0 {
					fmt.Printf(" max_units=%d", cfg.MaxUnits)
				}
				if cfg.MaxPositionUSD > 0 {
					fmt.Printf(" max_position_usd=%.2f", cfg.MaxPositionUSD)
				}
				fmt.Println()
				return nil
			}

			// Token: explicit flag > global config > env var.
			tok := token
			if !cmd.Flags().Changed("token") {
				if rc.OANDAToken != "" {
					tok = rc.OANDAToken
				} else {
					tok = os.Getenv("OANDA_TOKEN")
				}
			}

			// Account: explicit flag > global config > env var.
			resolvedAccount := accountID
			if !cmd.Flags().Changed("account-id") {
				if rc.OANDAAccountID != "" {
					resolvedAccount = rc.OANDAAccountID
				} else {
					resolvedAccount = os.Getenv("OANDA_ACCOUNT_ID")
				}
			}

			// Build service with OANDA.
			svc, err := service.New(service.Config{
				Env:       cfg.Env,
				Token:     tok,
				AccountID: resolvedAccount,
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

			// Prevent two bots from trading the same account simultaneously.
			_, release, err := acquireAccountLock(svc.AccountID)
			if err != nil {
				return err
			}
			defer release()

			// Build strategy — candle-based strategies need the OANDA client.
			strategy, err := buildStrategy(cfg, svc)
			if err != nil {
				return fmt.Errorf("build strategy: %w", err)
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

// buildStrategy delegates to the service layer so the same factory is used
// by both the CLI and the REST API bot manager.
func buildStrategy(cfg liveRunConfig, svc *service.Service) (trader.LiveStrategy, error) {
	// Top-level liveRunConfig fields seed params when not already set explicitly.
	p := cfg.Strategy.Params
	if p == nil {
		p = map[string]any{}
	}
	if cfg.MaxPositions > 0 {
		if _, set := p["max_positions"]; !set {
			p["max_positions"] = cfg.MaxPositions
		}
	}
	if cfg.RiskPct > 0 {
		if _, set := p["risk_pct"]; !set {
			p["risk_pct"] = cfg.RiskPct
		}
	}
	return svc.BuildLiveStrategy(service.StrategyConfig{
		Kind:        cfg.Strategy.Kind,
		Granularity: cfg.Strategy.Granularity,
		Params:      p,
	}, cfg.Instrument)
}
