package backtest

import (
	"context"
	"strings"
	"time"

	"github.com/rustyeddy/trader/account"
	bt "github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/strategies"
	"github.com/rustyeddy/trader/types"
	"github.com/spf13/cobra"
)

var tmplStrategyOpts = newCandleCmdCommon()

func runTmplStrategyConfig(cmd *cobra.Command) error {
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

	cfg, err := BuildTemplateStrategyConfig(*rr)
	if err != nil {
		return err
	}

	strat := strategies.NewTemplateStrategy(cfg)
	act := &account.Account{}
	return runCandleStrategy(
		context.Background(),
		tmplStrategyOpts,
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
		act,
	)
}
