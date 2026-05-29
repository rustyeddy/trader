package live

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/service"
)

func newPortfolioCmd(_ any) *cobra.Command {
	var (
		configFile string
		accountID  string
		token      string
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "portfolio",
		Short: "Run a multi-instrument live portfolio",
		Long: `Run a configured portfolio of strategies concurrently against OANDA.
Each instrument runs in its own goroutine. A shared drawdown circuit breaker
halts new opens if account equity falls below the configured threshold.

Example:
  trader live portfolio --config configs/demo-portfolio.yml
  trader live portfolio --config configs/demo-portfolio.yml --dry-run
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := notifyContext(cmd.Context())
			defer cancel()

			if configFile == "" {
				return fmt.Errorf("--config is required")
			}

			cfg, err := service.LoadPortfolioConfig(configFile)
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Printf("Portfolio dry-run: env=%s risk_pct=%.1f%% drawdown_circuit=%.1f%% instruments=%d\n",
					cfg.Env, cfg.RiskPct, cfg.DrawdownCircuitPct, len(cfg.Instruments))
				for _, inst := range cfg.Instruments {
					r := inst.RiskPct
					if r <= 0 {
						r = cfg.RiskPct
					}
					fmt.Printf("  %-10s %-4s  strategy=%-30s  risk=%.1f%%\n",
						inst.Instrument, inst.Timeframe, inst.Strategy.Kind, r)
				}
				return nil
			}

			// Config file account_id takes precedence over flag / env var.
			resolvedAccount := accountID
			if cfg.AccountID != "" {
				resolvedAccount = cfg.AccountID
			}

			svc, err := service.New(service.Config{
				Env:       cfg.Env,
				Token:     token,
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

			_, release, err := acquireAccountLock(svc.AccountID)
			if err != nil {
				return err
			}
			defer release()

			log := slog.Default()
			portfolioCfg, err := service.BuildPortfolioRunConfig(cfg, svc.OANDA, svc.AccountID, log)
			if err != nil {
				return fmt.Errorf("build portfolio config: %w", err)
			}

			fmt.Printf("Starting portfolio: %d instruments | account=%s | env=%s\n",
				len(portfolioCfg.Instruments), svc.AccountID, cfg.Env)
			for _, inst := range portfolioCfg.Instruments {
				fmt.Printf("  %-10s  %-4s  %s\n", inst.Instrument, inst.Granularity, inst.Strategy.Name())
			}
			fmt.Println("Press Ctrl-C to stop.")

			return svc.RunPortfolio(ctx, *portfolioCfg)
		},
	}

	cmd.Flags().StringVar(&configFile, "config", "", "Path to portfolio YAML config (required)")
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print resolved config and exit without trading")

	return cmd
}
