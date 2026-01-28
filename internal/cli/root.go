package cli

import (
	"fmt"
	"os"

	"github.com/rustyeddy/trader/internal/cli/backtest"
	"github.com/rustyeddy/trader/internal/cli/config"
	"github.com/rustyeddy/trader/internal/cli/replay"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	rc := &config.RootConfig{}

	cmd := &cobra.Command{
		Use:           "trader",
		Short:         "Trader — backtesting, replay, and data tooling",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global / persistent flags
	cmd.PersistentFlags().StringVar(&rc.ConfigPath, "config", "", "Path to config file (optional)")
	cmd.PersistentFlags().StringVar(&rc.DBPath, "db", "./trader.sqlite", "SQLite journal database")
	cmd.PersistentFlags().StringVar(&rc.LogLevel, "log-level", "info", "Log level: debug|info|warn|error")
	cmd.PersistentFlags().BoolVar(&rc.NoColor, "no-color", false, "Disable colored output")

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Intentionally minimal for now.
		// Later: config load + logging setup.
		return nil
	}

	// Subcommands
	cmd.AddCommand(
		backtest.New(rc),
		replay.New(rc),
	)

	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("trader (dev)")
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
