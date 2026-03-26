package backtest

import (
	"context"
	"fmt"
	"strings"
	"time"

	bt "github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/market"
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
	emaCrossCfg.Scale = types.PriceScale
	emaCrossCfg.Stop = emaCrossOpts.stopPips()
	emaCrossCfg.Take = emaCrossOpts.takePips()

	strat := strategies.NewEMACross(emaCrossCfg)

	return runCandleStrategy(
		context.Background(),
		emaCrossOpts,
		strat,
		candleRunMeta{
			RunID:    types.NewULID(),
			RunName:  "adhoc-ema-cross",
			Kind:     "ema-cross",
			Created:  types.FromTime(time.Now().UTC()),
			Balance:  emaCrossCfg.Balance,
			RR:       emaCrossCfg.RR,
			Strategy: strat.Name(),
		},
	)
}

func runEMACrossFromConfig(cmd *cobra.Command) error {
	path := strings.TrimSpace(rootCfg.ConfigPath)
	bcfg, err := bt.LoadConfig(path)
	if err != nil {
		return err
	}

	runName, err := selectConfigRunByKind(bcfg, btRunName, "ema-cross")
	if err != nil {
		return err
	}

	rr, err := bcfg.ResolveRun(runName)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(rr.Strategy.Kind), "ema-cross") {
		return fmt.Errorf("run %q strategy.kind=%q, want %q", rr.Name, rr.Strategy.Kind, "ema-cross")
	}

	cfg, err := BuildEMACrossConfig(*rr)
	if err != nil {
		return err
	}

	applyCommonOptsFromResolvedRun(&emaCrossOpts, rr)
	applyCommonFlagOverrides(cmd, &emaCrossOpts)
	applyEMACrossFlagOverrides(cmd, &cfg)

	if emaCrossOpts.Units32 == 0 {
		return fmt.Errorf("units resolved to 0; set defaults.units or strategy.params.units until risk-based sizing is implemented")
	}

	emaCrossOpts.Instrument = market.NormalizeInstrument(emaCrossOpts.Instrument)
	cfg.Scale = types.PriceScale
	cfg.Stop = emaCrossOpts.stopPips()
	cfg.Take = emaCrossOpts.takePips()

	strat := strategies.NewEMACross(cfg)

	return runCandleStrategy(
		context.Background(),
		emaCrossOpts,
		strat,
		candleRunMeta{
			RunID:    types.NewULID(),
			RunName:  rr.Name,
			Kind:     rr.Strategy.Kind,
			Created:  types.FromTime(time.Now().UTC()),
			Balance:  cfg.Balance,
			RR:       cfg.RR,
			Strategy: strat.Name(),
		},
	)
}

func applyEMACrossFlagOverrides(cmd *cobra.Command, cfg *strategies.EMACrossConfig) {
	if cmd.Flags().Changed("fast") {
		cfg.FastPeriod = emaCrossCfg.FastPeriod
	}
	if cmd.Flags().Changed("slow") {
		cfg.SlowPeriod = emaCrossCfg.SlowPeriod
	}
	if cmd.Flags().Changed("min-spread") {
		cfg.MinSpread = emaCrossCfg.MinSpread
	}
}
