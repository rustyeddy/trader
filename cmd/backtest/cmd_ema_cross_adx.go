package backtest

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	if rootCfg != nil && strings.TrimSpace(rootCfg.ConfigPath) != "" {
		return runEMACrossADXFromConfig(cmd)
	}
	return runEMACrossADXFromFlags(cmd)
}

func runEMACrossADXFromFlags(cmd *cobra.Command) error {
	emaCrossADXCfg.Scale = trader.PriceScale

	strat := trader.NewEMACrossADX(emaCrossADXCfg)
	act := trader.NewAccount("adhoc-ema-cross-adx", trader.MoneyFromFloat(1000))
	return runCandleStrategy(
		context.Background(),
		emaCrossADXOpts,
		strat,
		candleRunMeta{
			RunID:    trader.NewULID(),
			RunName:  "adhoc-ema-cross-adx",
			Kind:     "ema-cross-adx",
			Created:  trader.FromTime(time.Now().UTC()),
			Balance:  act.Balance,
			RR:       0,
			Strategy: strat.Name(),
		},
		act,
	)
}

func runEMACrossADXFromConfig(cmd *cobra.Command) error {
	path := strings.TrimSpace(rootCfg.ConfigPath)
	bcfg, err := trader.LoadConfig(path)
	if err != nil {
		return err
	}

	runName, err := selectConfigRunByKind(bcfg, btRunName, "ema-cross-adx")
	if err != nil {
		return err
	}

	rr, err := bcfg.ResolveRun(runName)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(rr.Strategy.Kind), "ema-cross-adx") {
		return fmt.Errorf("run %q strategy.kind=%q, want %q", rr.Name, rr.Strategy.Kind, "ema-cross-adx")
	}

	applyCommonOptsFromResolvedRun(&emaCrossADXOpts, rr)
	applyCommonFlagOverrides(cmd, &emaCrossADXOpts)
	applyEMACrossADXRunParamOverrides(cmd, rr)

	if emaCrossADXOpts.Units == 0 {
		return fmt.Errorf("units resolved to 0; set defaults.units or strategy.params.units until risk-based sizing is implemented")
	}

	emaCrossADXOpts.Instrument = trader.NormalizeInstrument(emaCrossADXOpts.Instrument)
	rr.Instrument = emaCrossADXOpts.Instrument

	strat, err := trader.NewStrategyFromResolvedRun(*rr)
	if err != nil {
		return err
	}
	act := trader.NewAccount(rr.Name, rr.StartingBalance)
	return runCandleStrategy(
		context.Background(),
		emaCrossADXOpts,
		strat,
		candleRunMeta{
			RunID:    trader.NewULID(),
			RunName:  rr.Name,
			Kind:     rr.Strategy.Kind,
			Created:  trader.FromTime(time.Now().UTC()),
			Balance:  rr.StartingBalance,
			RR:       rr.RR,
			Strategy: strat.Name(),
		},
		act,
	)
}

func applyCommonOptsFromResolvedRun(o *candleCmdCommon, r *trader.ResolvedRun) {
	o.Instrument = r.Instrument
	o.Timeframe = r.Timeframe
	o.From = r.From
	o.To = r.To
	o.Units = r.Units.Int64()
	o.StopPips = int32(r.StopPips)
	o.TakePips = int32(r.TakePips)
	o.RiskPct64 = r.RiskPct.Float64() * 100.0
}

func applyCommonFlagOverrides(cmd *cobra.Command, o *candleCmdCommon) {
	if cmd.Flags().Changed("instrument") {
		o.Instrument = trader.NormalizeInstrument(o.Instrument)
	}
	if cmd.Flags().Changed("timeframe") {
		o.Timeframe = strings.ToUpper(strings.TrimSpace(o.Timeframe))
	}
	if cmd.Flags().Changed("from") {
		o.From = strings.TrimSpace(o.From)
	}
	if cmd.Flags().Changed("to") {
		o.To = strings.TrimSpace(o.To)
	}
}

func applyEMACrossADXRunParamOverrides(cmd *cobra.Command, rr *trader.ResolvedRun) {
	if rr.Strategy.Params == nil {
		rr.Strategy.Params = make(map[string]any)
	}

	if cmd.Flags().Changed("fast") {
		rr.Strategy.Params["fast"] = emaCrossADXCfg.FastPeriod
	}
	if cmd.Flags().Changed("slow") {
		rr.Strategy.Params["slow"] = emaCrossADXCfg.SlowPeriod
	}
	if cmd.Flags().Changed("adx-period") {
		rr.Strategy.Params["adx_period"] = emaCrossADXCfg.ADXPeriod
	}
	if cmd.Flags().Changed("adx-threshold") {
		rr.Strategy.Params["adx_threshold"] = emaCrossADXCfg.ADXThreshold
	}
	if cmd.Flags().Changed("require-di") {
		rr.Strategy.Params["require_di"] = emaCrossADXCfg.RequireDI
	}
	if cmd.Flags().Changed("require-adx-ready") {
		rr.Strategy.Params["require_adx_ready"] = emaCrossADXCfg.RequireADXReady
	}
	if cmd.Flags().Changed("min-spread") {
		rr.Strategy.Params["min_spread"] = emaCrossADXCfg.MinSpread
	}
}
