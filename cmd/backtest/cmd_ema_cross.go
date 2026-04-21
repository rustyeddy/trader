package backtest

import (
	"context"
	"strings"
	"time"

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
	if rootCfg != nil && strings.TrimSpace(rootCfg.ConfigPath) != "" {
		return runEMACrossFromConfig(cmd)
	}
	return runEMACrossFromFlags(cmd)
}

func runEMACrossFromFlags(cmd *cobra.Command) error {
	emaCrossCfg.Scale = trader.PriceScale

	strat := trader.NewEMACross(emaCrossCfg)
	act := trader.NewAccount("adhoc-ema-cross", trader.MoneyFromFloat(1000))
	return runCandleStrategy(
		context.Background(),
		emaCrossOpts,
		strat,
		candleRunMeta{
			RunID:    trader.NewULID(),
			RunName:  "adhoc-ema-cross",
			Kind:     "ema-cross",
			Created:  trader.FromTime(time.Now().UTC()),
			Balance:  act.Balance,
			RR:       0,
			Strategy: strat.Name(),
		},
		act,
	)
}

func runEMACrossFromConfig(cmd *cobra.Command) error {
	return runConfiguredStrategyCommand(cmd, "ema-cross", &emaCrossOpts, applyEMACrossRunParamOverrides)
}

func applyEMACrossRunParamOverrides(cmd *cobra.Command, rr *trader.ResolvedRun) {
	if rr.Strategy.Params == nil {
		rr.Strategy.Params = make(map[string]any)
	}

	if cmd.Flags().Changed("fast") {
		rr.Strategy.Params["fast"] = emaCrossCfg.FastPeriod
	}
	if cmd.Flags().Changed("slow") {
		rr.Strategy.Params["slow"] = emaCrossCfg.SlowPeriod
	}
	if cmd.Flags().Changed("min-spread") {
		rr.Strategy.Params["min_spread"] = emaCrossCfg.MinSpread
	}
}
