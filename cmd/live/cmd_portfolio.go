package live

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	trader "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
)

func newPortfolioCmd(rc *trader.RootConfig) *cobra.Command {
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
			fmt.Fprintln(os.Stderr, "DEPRECATED: 'trader live portfolio' is deprecated.")
			fmt.Fprintln(os.Stderr, "  Use 'trader serve' then 'trader bot start --config <yaml>' instead.")
			fmt.Fprintln(os.Stderr, "  The standalone portfolio runner will be removed in a future release.")
			fmt.Fprintln(os.Stderr)

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

			// Token: explicit flag > global config > env var.
			tok := token
			if !cmd.Flags().Changed("token") {
				if rc.OANDAToken != "" {
					tok = rc.OANDAToken
				} else {
					tok = os.Getenv("OANDA_TOKEN")
				}
			}

			// Account: explicit flag > YAML config > global config > env var.
			resolvedAccount := ""
			if cmd.Flags().Changed("account-id") {
				resolvedAccount = accountID
			}
			if resolvedAccount == "" {
				resolvedAccount = cfg.AccountID
			}
			if resolvedAccount == "" {
				resolvedAccount = rc.OANDAAccountID
			}
			if resolvedAccount == "" {
				resolvedAccount = os.Getenv("OANDA_ACCOUNT_ID")
			}

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

			_, release, err := acquireAccountLock(svc.AccountID)
			if err != nil {
				return err
			}
			defer release()

			log := trader.L
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
