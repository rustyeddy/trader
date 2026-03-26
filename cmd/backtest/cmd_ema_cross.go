package backtest

import (
	"context"

	"github.com/rustyeddy/trader/market/strategies"
	"github.com/rustyeddy/trader/types"
	"github.com/spf13/cobra"
)

var CMDBacktestEMACross = &cobra.Command{
	Use:   "ema-cross",
	Short: "Run EMA cross strategy",
	RunE:  RunEMACross,
}

var emaCrossOpts = newCandleCmdCommon()
var emaCrossCfg = strategies.EMACrossConfig{}

func init() {
	scfg := strategies.StrategyConfig{
		Balance: types.MoneyFromFloat(1000),
		Stop:    20,
		Take:    40,
		RR:      types.RateFromFloat(2.0),
	}
	emaCrossCfg.StrategyConfig = scfg

	cmd := CMDBacktestEMACross
	emaCrossOpts.addFlags(cmd)
	cmd.Flags().IntVar(&emaCrossCfg.FastPeriod, "fast", 12, "Fast EMA period")
	cmd.Flags().IntVar(&emaCrossCfg.SlowPeriod, "slow", 26, "Slow EMA period")
	cmd.Flags().Float64Var(&emaCrossCfg.MinSpread, "min-spread", 0, "Min |fast-slow| required to signal; 0 disables")
}

func RunEMACross(cmd *cobra.Command, args []string) error {
	emaCrossCfg.Scale = types.PriceScale
	emaCrossCfg.Stop = emaCrossOpts.stopPips()
	emaCrossCfg.Take = emaCrossOpts.takePips()

	strat := strategies.NewEMACross(emaCrossCfg)

	return runCandleStrategy(
		context.Background(),
		emaCrossOpts,
		strat,
		candleRunMeta{
			Balance:  emaCrossCfg.Balance,
			RR:       emaCrossCfg.RR,
			Strategy: strat.Name(),
		},
	)
}
