package main

import (
	"fmt"
	"os"

	traderpkg "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/cmd/api"
	"github.com/rustyeddy/trader/cmd/backtest"
	cmdmcp "github.com/rustyeddy/trader/cmd/mcp"
	"github.com/rustyeddy/trader/cmd/serve"
	"github.com/rustyeddy/trader/cmd/data"
	"github.com/rustyeddy/trader/cmd/live"
	"github.com/rustyeddy/trader/cmd/order"
	"github.com/rustyeddy/trader/cmd/replay"
	"github.com/spf13/cobra"

	// Provider registration via init().
	_ "github.com/rustyeddy/trader/data/dukascopy"

	// Strategy registration via init().
	_ "github.com/rustyeddy/trader/strategies/bollingerfade"
	_ "github.com/rustyeddy/trader/strategies/donchian"
	_ "github.com/rustyeddy/trader/strategies/donchianv2"
	_ "github.com/rustyeddy/trader/strategies/donchianv3"
	_ "github.com/rustyeddy/trader/strategies/donchianv4"
	_ "github.com/rustyeddy/trader/strategies/donchianv5"
	_ "github.com/rustyeddy/trader/strategies/donchianv6"
	_ "github.com/rustyeddy/trader/strategies/emacross"
	_ "github.com/rustyeddy/trader/strategies/emacrossadx"
	_ "github.com/rustyeddy/trader/strategies/fake"
	_ "github.com/rustyeddy/trader/strategies/lifecycle"
	_ "github.com/rustyeddy/trader/strategies/noop"
	_ "github.com/rustyeddy/trader/strategies/tmpl"
)

func NewRootCmd() *cobra.Command {
	rc := &traderpkg.RootConfig{}

	cmd := &cobra.Command{
		Use:           "trader",
		Short:         "Trader — backtesting, replay, and data tooling",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global / persistent flags
	cmd.PersistentFlags().StringVar(&rc.ConfigPath, "config", "", "Path to config file or directory (optional)")
	cmd.PersistentFlags().StringVar(&rc.DBPath, "db", "./trader.db", "SQLite journal database")
	cmd.PersistentFlags().StringVar(&rc.ReportPath, "report", "", "backtest report path")
	cmd.PersistentFlags().StringVar(&rc.DataDir, "data-dir", "/srv/trading/data/candles", "Root directory for candle data")
	cmd.PersistentFlags().StringVar(&rc.LogLevel, "log-level", "debug", "Log level: debug|info|warn|error")
	cmd.PersistentFlags().StringVar(&rc.LogFormat, "log-format", "text", "Log format: text|json")
	cmd.PersistentFlags().StringVar(&rc.LogFile, "log-file", "", "Path to log file (written in addition to stdout)")
	cmd.PersistentFlags().BoolVar(&rc.NoColor, "no-color", false, "Disable colored output")

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Load global config files, then apply fields that the user did not
		// explicitly set via CLI flags (flags always win).
		gcfg, err := traderpkg.LoadGlobalConfig(rc.ConfigPath)
		if err != nil {
			return fmt.Errorf("global config: %w", err)
		}
		flags := cmd.Flags()
		if !flags.Changed("log-level") && gcfg.Log.Level != "" {
			rc.LogLevel = gcfg.Log.Level
		}
		if !flags.Changed("log-format") && gcfg.Log.Format != "" {
			rc.LogFormat = gcfg.Log.Format
		}
		if !flags.Changed("log-file") && gcfg.Log.File != "" {
			rc.LogFile = gcfg.Log.File
		}
		if !flags.Changed("data-dir") && gcfg.Data.Dir != "" {
			rc.DataDir = gcfg.Data.Dir
		}
		if !flags.Changed("db") && gcfg.DB != "" {
			rc.DBPath = gcfg.DB
		}
		// OANDA creds have no root-level CLI flags; always take from global config.
		if gcfg.OANDA.Token != "" {
			rc.OANDAToken = gcfg.OANDA.Token
		}
		if gcfg.OANDA.AccountID != "" {
			rc.OANDAAccountID = gcfg.OANDA.AccountID
		}
		if gcfg.OANDA.Env != "" {
			rc.OANDAEnv = gcfg.OANDA.Env
		}

		traderpkg.SetDataDir(rc.DataDir)
		return traderpkg.Setup(traderpkg.LogConfig{
			Level:  rc.LogLevel,
			Format: rc.LogFormat,
			File:   rc.LogFile,
			Stdout: true,
		})
	}

	// Subcommands
	cmd.AddCommand(
		api.New(rc),
		backtest.New(rc),
		cmdmcp.New(rc),
		serve.New(rc),
		data.New(rc),
		live.New(rc),
		order.New(),
		replay.New(rc),
	)

	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("trader %s\n", traderpkg.Version)
		},
	})

	return cmd
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func main() {
	Execute()
}
