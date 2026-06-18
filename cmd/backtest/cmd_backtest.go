package backtest

import (
	traderpkg "github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

var rootCfg *traderpkg.RootConfig
var l = traderpkg.L

func New(rc *traderpkg.RootConfig) *cobra.Command {
	rootCfg = rc
	return CMDBacktest
}

func init() {
	CMDBacktest.AddCommand(CMDBacktestRun)
	CMDBacktest.AddCommand(CMDBacktestRegress)
	CMDBacktest.AddCommand(CMDBacktestList)
	CMDBacktest.AddCommand(CMDBacktestGet)
	CMDBacktest.AddCommand(CMDBacktestOrg)
	CMDBacktest.AddCommand(CMDBacktestCandles)
	CMDBacktest.AddCommand(CMDBacktestConfigs)
}

var CMDBacktest = &cobra.Command{
	Use:   "backtest",
	Short: "Backtest commands",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
