package backtest

import (
	"strings"

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
	CMDBacktest.AddCommand(CMDBacktestEMACross)
	CMDBacktest.AddCommand(CMDBacktestEMACrossADX)
	CMDBacktest.Flags().StringVar(&btReportsDir, "reports", defaultReportsDir, "Directory for generated backtest reports")
}

var CMDBacktest = &cobra.Command{
	Use:   "backtest",
	Short: "Run backtests on historical data",
	RunE: func(cmd *cobra.Command, args []string) error {
		if rootCfg != nil && strings.TrimSpace(rootCfg.ConfigPath) != "" {
			return runBacktestConfigBatch(cmd)
		}
		return cmd.Help()
	},
}
