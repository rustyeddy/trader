package backtest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/account"
	bt "github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/strategies"
	"github.com/rustyeddy/trader/types"
	"github.com/spf13/cobra"
)

var CMDBacktestEMACrossADX = &cobra.Command{
	Use:   "ema-cross-adx",
	Short: "Run EMA cross strategy gated by ADX",
	RunE:  RunEMACrossADX,
}

var emaCrossADXOpts = newCandleCmdCommon()
var emaCrossADXCfg = strategies.EMACrossADXConfig{}

func init() {
	emaCrossADXCfg.StrategyConfig = strategies.StrategyConfig{}

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
	emaCrossADXCfg.Scale = types.PriceScale

	strat := strategies.NewEMACrossADX(emaCrossADXCfg)
	act := account.NewAccount("adhoc-ema-cross-adx", types.MoneyFromFloat(1000))
	return runCandleStrategy(
		context.Background(),
		emaCrossADXOpts,
		strat,
		candleRunMeta{
			RunID:    types.NewULID(),
			RunName:  "adhoc-ema-cross-adx",
			Kind:     "ema-cross-adx",
			Created:  types.FromTime(time.Now().UTC()),
			Balance:  act.Balance,
			RR:       0,
			Strategy: strat.Name(),
		},
		act,
	)
}

func runEMACrossADXFromConfig(cmd *cobra.Command) error {
	path := strings.TrimSpace(rootCfg.ConfigPath)
	bcfg, err := bt.LoadConfig(path)
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

	cfg, err := buildEMACrossADXConfig(*rr)
	if err != nil {
		return err
	}

	applyCommonOptsFromResolvedRun(&emaCrossADXOpts, rr)
	applyCommonFlagOverrides(cmd, &emaCrossADXOpts)
	applyEMACrossADXFlagOverrides(cmd, &cfg)

	if emaCrossADXOpts.Units == 0 {
		return fmt.Errorf("units resolved to 0; set defaults.units or strategy.params.units until risk-based sizing is implemented")
	}

	emaCrossADXOpts.Instrument = types.NormalizeInstrument(emaCrossADXOpts.Instrument)
	cfg.Scale = types.PriceScale

	strat := strategies.NewEMACrossADX(cfg)
	act := account.NewAccount(rr.Name, rr.StartingBalance)
	return runCandleStrategy(
		context.Background(),
		emaCrossADXOpts,
		strat,
		candleRunMeta{
			RunID:    types.NewULID(),
			RunName:  rr.Name,
			Kind:     rr.Strategy.Kind,
			Created:  types.FromTime(time.Now().UTC()),
			Balance:  rr.StartingBalance,
			RR:       rr.RR,
			Strategy: strat.Name(),
		},
		act,
	)
}

func buildEMACrossADXConfig(r bt.ResolvedRun) (strategies.EMACrossADXConfig, error) {
	cfg := strategies.EMACrossADXConfig{}

	fast, ok, err := getRunIntParam(r.Strategy.Params, "fast")
	if err != nil {
		return cfg, err
	}
	if !ok || fast <= 0 {
		return cfg, fmt.Errorf("missing or invalid param %q", "fast")
	}

	slow, ok, err := getRunIntParam(r.Strategy.Params, "slow")
	if err != nil {
		return cfg, err
	}
	if !ok || slow <= 0 {
		return cfg, fmt.Errorf("missing or invalid param %q", "slow")
	}

	adxPeriod, ok, err := getRunIntParam(r.Strategy.Params, "adx_period")
	if err != nil {
		return cfg, err
	}
	if !ok || adxPeriod <= 0 {
		adxPeriod = 14
	}

	adxThreshold, ok, err := getRunFloatParam(r.Strategy.Params, "adx_threshold")
	if err != nil {
		return cfg, err
	}
	if !ok || adxThreshold <= 0 {
		adxThreshold = 20.0
	}

	minSpread, ok, err := getRunFloatParam(r.Strategy.Params, "min_spread")
	if err != nil {
		return cfg, err
	}
	if !ok {
		minSpread = 0
	}

	requireDI, ok, err := getRunBoolParam(r.Strategy.Params, "require_di")
	if err != nil {
		return cfg, err
	}
	if !ok {
		requireDI = false
	}

	requireADXReady, ok, err := getRunBoolParam(r.Strategy.Params, "require_adx_ready")
	if err != nil {
		return cfg, err
	}
	if !ok {
		requireADXReady = true
	}

	scale := r.Scale
	if scale <= 0 {
		scale = types.PriceScale
	}

	return strategies.EMACrossADXConfig{
		StrategyConfig:  strategies.StrategyConfig{},
		FastPeriod:      fast,
		SlowPeriod:      slow,
		ADXPeriod:       adxPeriod,
		Scale:           scale,
		MinSpread:       minSpread,
		ADXThreshold:    adxThreshold,
		RequireDI:       requireDI,
		RequireADXReady: requireADXReady,
	}, nil
}

func applyCommonOptsFromResolvedRun(o *candleCmdCommon, r *bt.ResolvedRun) {
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
		o.Instrument = types.NormalizeInstrument(o.Instrument)
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

func applyEMACrossADXFlagOverrides(cmd *cobra.Command, cfg *strategies.EMACrossADXConfig) {
	if cmd.Flags().Changed("fast") {
		cfg.FastPeriod = emaCrossADXCfg.FastPeriod
	}
	if cmd.Flags().Changed("slow") {
		cfg.SlowPeriod = emaCrossADXCfg.SlowPeriod
	}
	if cmd.Flags().Changed("adx-period") {
		cfg.ADXPeriod = emaCrossADXCfg.ADXPeriod
	}
	if cmd.Flags().Changed("adx-threshold") {
		cfg.ADXThreshold = emaCrossADXCfg.ADXThreshold
	}
	if cmd.Flags().Changed("require-di") {
		cfg.RequireDI = emaCrossADXCfg.RequireDI
	}
	if cmd.Flags().Changed("require-adx-ready") {
		cfg.RequireADXReady = emaCrossADXCfg.RequireADXReady
	}
	if cmd.Flags().Changed("min-spread") {
		cfg.MinSpread = emaCrossADXCfg.MinSpread
	}
}

func getRunIntParam(m map[string]any, key string) (int, bool, error) {
	v, ok := m[key]
	if !ok {
		return 0, false, nil
	}

	switch x := v.(type) {
	case int:
		return x, true, nil
	case int32:
		return int(x), true, nil
	case int64:
		return int(x), true, nil
	case float64:
		return int(x), true, nil
	default:
		return 0, true, fmt.Errorf("param %q must be numeric, got %T", key, v)
	}
}

func getRunFloatParam(m map[string]any, key string) (float64, bool, error) {
	v, ok := m[key]
	if !ok {
		return 0, false, nil
	}

	switch x := v.(type) {
	case float64:
		return x, true, nil
	case float32:
		return float64(x), true, nil
	case int:
		return float64(x), true, nil
	case int32:
		return float64(x), true, nil
	case int64:
		return float64(x), true, nil
	default:
		return 0, true, fmt.Errorf("param %q must be numeric, got %T", key, v)
	}
}

func getRunBoolParam(m map[string]any, key string) (bool, bool, error) {
	v, ok := m[key]
	if !ok {
		return false, false, nil
	}

	x, ok := v.(bool)
	if !ok {
		return false, true, fmt.Errorf("param %q must be bool, got %T", key, v)
	}
	return x, true, nil
}
