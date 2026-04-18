package backtest

import (
	"context"
	"strings"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
)

var tmplStrategyOpts = newCandleCmdCommon()

func runTmplStrategyConfig(cmd *cobra.Command) error {
	path := strings.TrimSpace(rootCfg.ConfigPath)
	bcfg, err := trader.LoadConfig(path)
	if err != nil {
		return err
	}

	runName, err := selectConfigRunByKind(bcfg, btRunName, "template")
	if err != nil {
		return err
	}

	rr, err := bcfg.ResolveRun(runName)
	if err != nil {
		return err
	}

	cfg, err := BuildTemplateStrategyConfig(*rr)
	if err != nil {
		return err
	}

	strat := trader.NewTemplateStrategy(cfg)
	act := trader.NewAccount(rr.Name, rr.StartingBalance)
	return runCandleStrategy(
		context.Background(),
		tmplStrategyOpts,
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
