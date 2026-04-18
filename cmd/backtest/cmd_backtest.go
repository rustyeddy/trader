package backtest

import (
	traderpkg "github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

var rootCfg *traderpkg.RootConfig

func New(rc *traderpkg.RootConfig) *cobra.Command {
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
