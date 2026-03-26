package backtest

import (
	cmdconfig "github.com/rustyeddy/trader/cmd/config"
	"github.com/spf13/cobra"
)

var rootCfg *cmdconfig.RootConfig

func New(rc *cmdconfig.RootConfig) *cobra.Command {
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
