// Package mcp hosts the CLI command for starting the MCP stdio server.
// This runs as a subprocess spawned by Claude Code or Claude Desktop;
// it is not a daemon and must not be combined with trader serve.
package mcp

import (
	"os"

	"github.com/spf13/cobra"

	mcpserver "github.com/rustyeddy/trader/api/mcp"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/log"
)

// New returns the "mcp" cobra command. It runs the MCP server directly
// on stdio — there is no subcommand layer.
func New(rc *config.RootConfig) *cobra.Command {
	var (
		accountID  string
		reportsDir string
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

Available tools:
  list_accounts      List OANDA accounts the configured token can access
  account_summary    Mirrors 'trader account summary'
  account_orders     Mirrors 'trader account orders'
  get_version        Server version
  get_health         Server health

Resources:
  backtest://results    List or read backtest .org reports
  config://configs      List or read YAML backtest configs

This is deliberately minimal — tools are added on a use-case basis, not for
parity with the CLI/REST surface. Account tools resolve their own OANDA
broker from OANDA_TOKEN/~/.config/oanda/pat.txt, same as the CLI.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Account: explicit flag > global config > env var.
			resolvedAccount := accountID
			if !cmd.Flags().Changed("account-id") && rc.OANDA.AccountID != "" {
				resolvedAccount = rc.OANDA.AccountID
			}

			srv := mcpserver.New(log.L, resolvedAccount)
			if reportsDir != "" {
				srv.WithReportsDir(reportsDir)
			}
			return srv.ServeStdio(ctx)
		},
	}

	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID")
	cmd.Flags().StringVar(&reportsDir, "reports-dir", "/srv/trading/backtests/reports", "Backtest reports directory")

	return cmd
}
