package backtest

import (
	traderpkg "github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

var rootCfg *traderpkg.RootConfig
var btReportsDir string

const defaultReportsDir = "reports"

func New(rc *traderpkg.RootConfig) *cobra.Command {
	rootCfg = rc
	return CMDBacktest
}

func init() {
	// Patch 1:
	// Keep only the regression/config-driven backtest entry point.
	CMDBacktest.AddCommand(CMDBacktestRegress)

	CMDBacktest.Flags().StringVar(
		&btReportsDir,
		"reports",
		defaultReportsDir,
		"Directory for generated backtest reports",
	)
}

var CMDBacktest = &cobra.Command{
	Use:   "backtest",
	Short: "Backtest commands",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
