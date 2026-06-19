// Package mcp hosts the CLI command for starting the MCP stdio server.
// This runs as a subprocess spawned by Claude Code or Claude Desktop;
// it is not a daemon and must not be combined with trader serve.
package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
	mcpserver "github.com/rustyeddy/trader/api/mcp"
	"github.com/rustyeddy/trader/service"
)

// New returns the "mcp" cobra command. It runs the MCP server directly
// on stdio — there is no subcommand layer.
func New(rc *trader.RootConfig) *cobra.Command {
	var (
		token       string
		accountID   string
		env         string
		writeEnable bool
		reportsDir  string
	)

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server: expose trader as typed Claude tools (stdio transport)",
		Long: `Start the trader MCP server using stdio transport.

Add to ~/.claude/mcp_servers.json or your Claude Desktop config:
  {
    "trader": {
      "command": "trader",
      "args": ["mcp"]
    }
  }

Without --token, only backtest tools are available.
With --token, live account/trade tools are enabled.
Write tools (place_order, close_trade, update_stop) require --enable-write.

Available tools (read-only by default):
  get_account_summary   Current balance, NAV, margin, P/L
  list_open_trades      All open OANDA positions
  get_transactions      Account transaction history
  run_backtest          Run YAML config(s) and return summaries
  list_bots             List all live strategy bots (running and stopped)
  get_bot               Get status of a single bot by ID

Additional tools when --enable-write:
  place_order           Size and submit a market order
  close_trade           Close a position fully or partially
  update_stop           Move stop-loss or take-profit
  start_bot             Start a new live strategy bot
  stop_bot              Stop a running bot by ID

Resources:
  backtest://results    List or read backtest .org reports
  config://configs      List or read YAML backtest configs
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			log := trader.L

			// Token: explicit flag > global config > env var > token file.
			tok := token
			if !cmd.Flags().Changed("token") {
				if rc.OANDAToken != "" {
					tok = rc.OANDAToken
				} else {
					tok = resolveTokenFile()
				}
			}
			// Account: explicit flag > global config > env var.
			resolvedAccount := accountID
			if !cmd.Flags().Changed("account-id") && rc.OANDAAccountID != "" {
				resolvedAccount = rc.OANDAAccountID
			}

			var svc *service.Service
			if tok != "" {
				var err error
				svc, err = service.New(service.Config{
					Env:       env,
					Token:     tok,
					AccountID: resolvedAccount,
					Log:       log,
				})
				if err != nil {
					return fmt.Errorf("init service: %w", err)
				}
			} else {
				svc = &service.Service{Log: log}
			}

			srv := mcpserver.New(svc, writeEnable)
			if reportsDir != "" {
				srv.WithReportsDir(reportsDir)
			}
			return srv.ServeStdio(ctx)
		},
	}

	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (enables live endpoints)")
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().BoolVar(&writeEnable, "enable-write", false, "Enable write tools: place_order, close_trade, update_stop, start_bot, stop_bot")
	cmd.Flags().StringVar(&reportsDir, "reports-dir", "/srv/trading/backtests/reports", "Backtest reports directory")

	return cmd
}

// resolveTokenFile reads the OANDA token from ~/.config/oanda/pat.txt.
func resolveTokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "oanda", "pat.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
