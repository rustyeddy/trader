package backtest

import (
	"github.com/rustyeddy/trader/config"
	"github.com/spf13/cobra"
)

var rootCfg *config.RootConfig

func New(rc *config.RootConfig) *cobra.Command {
	rootCfg = rc
	return CMDBacktest
}

func init() {
	CMDBacktest.AddCommand(CMDBacktestEMACross)
	CMDBacktest.AddCommand(CMDBacktestEMACrossADX)
}

var CMDBacktest = &cobra.Command{
	Use:   "backtest",
	Short: "Run backtests on historical data",
}
