// Package mcp hosts the CLI command for starting the MCP server.
package mcp

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
	mcpserver "github.com/rustyeddy/trader/api/mcp"
	"github.com/rustyeddy/trader/service"
)

// New returns the top-level "mcp" cobra command.
func New(rc *trader.RootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server: expose trader as typed Claude tools (stdio transport)",
	}
	cmd.AddCommand(newServeCmd(rc))
	return cmd
}

func newServeCmd(rc *trader.RootConfig) *cobra.Command {
	var (
		token       string
		accountID   string
		env         string
		writeEnable bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server on stdio (for Claude Code / Claude Desktop)",
		Long: `Start the trader MCP server using stdio transport.

Add to ~/.claude/mcp_servers.json or your Claude Desktop config:
  {
    "trader": {
      "command": "trader",
      "args": ["mcp", "serve"]
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

Additional tools when --enable-write:
  place_order           Size and submit a market order
  close_trade           Close a position fully or partially
  update_stop           Move stop-loss or take-profit

Resources:
  backtest://results    List or read backtest .org reports
  config://configs      List or read YAML backtest configs
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
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
			} else {
				svc = &service.Service{Log: log}
			}

			srv := mcpserver.New(svc, writeEnable)
			return srv.ServeStdio(ctx)
		},
	}

	cmd.Flags().StringVar(&token, "token", os.Getenv("OANDA_TOKEN"), "OANDA API token (enables live endpoints)")
	cmd.Flags().StringVar(&accountID, "account-id", os.Getenv("OANDA_ACCOUNT_ID"), "OANDA account ID")
	cmd.Flags().StringVar(&env, "env", "practice", "OANDA environment: practice|live")
	cmd.Flags().BoolVar(&writeEnable, "enable-write", false, "Enable write tools: place_order, close_trade, update_stop")

	return cmd
}

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
