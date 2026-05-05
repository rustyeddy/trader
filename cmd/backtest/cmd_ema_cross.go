package backtest

import (
	"github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

var CMDBacktestEMACross = &cobra.Command{
	Use:   "ema-cross",
	Short: "Run EMA cross strategy",
	RunE:  RunEMACross,
}

var emaCrossOpts = newCandleCmdCommon()
var emaCrossCfg = trader.EMACrossConfig{}

func init() {
	emaCrossCfg.StrategyBaseConfig = trader.StrategyBaseConfig{}

	cmd := CMDBacktestEMACross
	emaCrossOpts.addFlags(cmd)
	cmd.Flags().StringVar(&btRunName, "run", btRunName, "Named run from --config")

	cmd.Flags().IntVar(&emaCrossCfg.FastPeriod, "fast", 12, "Fast EMA period")
	cmd.Flags().IntVar(&emaCrossCfg.SlowPeriod, "slow", 26, "Slow EMA period")
	cmd.Flags().Float64Var(&emaCrossCfg.MinSpread, "min-spread", 0, "Min |fast-slow| required to signal; 0 disables")
}

func RunEMACross(cmd *cobra.Command, args []string) error {
	return nil
}
