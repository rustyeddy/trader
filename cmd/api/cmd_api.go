// Package api hosts the CLI command for starting the REST API server.
package api

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/api/rest"
	"github.com/rustyeddy/trader/service"
)

// New returns the top-level "api" cobra command.
func New(rc *trader.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "REST API server for trader (backtest + live trading)",
	}
	cmd.AddCommand(newServeCmd(rc))
	return cmd
}

func newServeCmd(rc *trader.RootConfig) *cobra.Command {
	var (
		addr      string
		token     string
		accountID string
		env       string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the REST API server",
		Long: `Start the trader REST API server.

Without --token the server starts in backtest-only mode (OANDA endpoints return 503).
With --token the server can place/manage live orders against OANDA.

Endpoints:
  GET    /api/v1/health
  GET    /api/v1/account
  GET    /api/v1/trades
  POST   /api/v1/trades
  PATCH  /api/v1/trades/{id}/stop
  DELETE /api/v1/trades/{id}
  GET    /api/v1/transactions
  POST   /api/v1/backtests/run
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			log := trader.L

			var svc *service.Service
			if tok := resolveToken(token); tok != "" {
				var err error
				svc, err = service.New(service.Config{
					Env:       env,
					Token:     tok,
					AccountID: accountID,
					Log:       log,
				})
				if err != nil {
					return fmt.Errorf("init service: %w", err)
				}
				log.Info("api: OANDA integration enabled", "env", env)
			} else {
				svc = &service.Service{Log: log}
				log.Info("api: starting in backtest-only mode (no OANDA token)")
			}

			srv := rest.New(svc, addr)
			fmt.Printf("REST API listening on %s\n", addr)
			return srv.Serve(ctx)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":8080", "TCP address to listen on")
	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (enables live order endpoints)")
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID (auto-discovered if omitted)")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")

	return cmd
}

// resolveToken checks the explicit flag first, then ~/.config/oanda/pat.txt.
func resolveToken(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(home + "/.config/oanda/pat.txt")
	if err != nil {
		return ""
	}
	s := string(data)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
