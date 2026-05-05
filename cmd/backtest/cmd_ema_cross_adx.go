package backtest

import (
	"github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

var CMDBacktestEMACrossADX = &cobra.Command{
	Use:   "ema-cross-adx",
	Short: "Run EMA cross strategy gated by ADX",
	RunE:  RunEMACrossADX,
}

var emaCrossADXOpts = newCandleCmdCommon()
var emaCrossADXCfg = trader.EMACrossADXConfig{}

func init() {
	emaCrossADXCfg.StrategyBaseConfig = trader.StrategyBaseConfig{}

	cmd := CMDBacktestEMACrossADX
	emaCrossADXOpts.addFlags(cmd)
	cmd.Flags().StringVar(&btRunName, "run", btRunName, "Named run from --config")

	cmd.Flags().IntVar(&emaCrossADXCfg.FastPeriod, "fast", 12, "Fast EMA period")
	cmd.Flags().IntVar(&emaCrossADXCfg.SlowPeriod, "slow", 26, "Slow EMA period")
	cmd.Flags().IntVar(&emaCrossADXCfg.ADXPeriod, "adx-period", 14, "ADX period")
	cmd.Flags().Float64Var(&emaCrossADXCfg.ADXThreshold, "adx-threshold", 20.0, "Minimum ADX required to allow signals")
	cmd.Flags().BoolVar(&emaCrossADXCfg.RequireDI, "require-di", false, "Require +DI/-DI directional confirmation")
	cmd.Flags().BoolVar(&emaCrossADXCfg.RequireADXReady, "require-adx-ready", true, "Require ADX readiness before signals")
	cmd.Flags().Float64Var(&emaCrossADXCfg.MinSpread, "min-spread", 0, "Min |fast-slow| required to signal; 0 disables")
}

func RunEMACrossADX(cmd *cobra.Command, args []string) error {
	return nil
}

// func runEMACrossADXFromFlags(cmd *cobra.Command) error {
// 	_ = cmd
// 	emaCrossADXCfg.Scale = trader.PriceScale
// 	return runAdhocStrategyCommand(
// 		emaCrossADXOpts,
// 		"adhoc-ema-cross-adx",
// 		"ema-cross-adx",
// 		trader.MoneyFromFloat(1000),
// 		func() trader.Strategy {
// 			return trader.NewEMACrossADX(emaCrossADXCfg)
// 		},
// 	)
// }

// func runEMACrossADXFromConfig(cmd *cobra.Command) error {
// 	return runConfiguredStrategyCommand(cmd, "ema-cross-adx", &emaCrossADXOpts, applyEMACrossADXRunParamOverrides)
// }

// func applyEMACrossADXRunParamOverrides(cmd *cobra.Command, rr *trader.ResolvedRun) {
// 	if rr.Strategy.Params == nil {
// 		rr.Strategy.Params = make(map[string]any)
// 	}

// 	if cmd.Flags().Changed("fast") {
// 		rr.Strategy.Params["fast"] = emaCrossADXCfg.FastPeriod
// 	}
// 	if cmd.Flags().Changed("slow") {
// 		rr.Strategy.Params["slow"] = emaCrossADXCfg.SlowPeriod
// 	}
// 	if cmd.Flags().Changed("adx-period") {
// 		rr.Strategy.Params["adx_period"] = emaCrossADXCfg.ADXPeriod
// 	}
// 	if cmd.Flags().Changed("adx-threshold") {
// 		rr.Strategy.Params["adx_threshold"] = emaCrossADXCfg.ADXThreshold
// 	}
// 	if cmd.Flags().Changed("require-di") {
// 		rr.Strategy.Params["require_di"] = emaCrossADXCfg.RequireDI
// 	}
// 	if cmd.Flags().Changed("require-adx-ready") {
// 		rr.Strategy.Params["require_adx_ready"] = emaCrossADXCfg.RequireADXReady
// 	}
// 	if cmd.Flags().Changed("min-spread") {
// 		rr.Strategy.Params["min_spread"] = emaCrossADXCfg.MinSpread
// 	}
// }
