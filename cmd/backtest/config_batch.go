package backtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/rustyeddy/trader"
)

func executeConfiguredRun(ctx context.Context, rr trader.ResolvedRun) (trader.BacktestRun, error) {
	opts := newCandleCmdCommon()
	applyCommonOptsFromResolvedRun(&opts, &rr)

	opts.Instrument = trader.NormalizeInstrument(opts.Instrument)

	meta := candleRunMeta{
		RunID:   trader.NewULID(),
		RunName: rr.Name,
		Kind:    rr.Strategy.Kind,
		Created: trader.FromTime(time.Now().UTC()),
		Balance: rr.StartingBalance,
		RR:      rr.RR,
	}

	acct := trader.NewAccount(rr.Name, rr.StartingBalance)
	strat, err := trader.NewStrategyFromResolvedRun(rr)
	if err != nil {
		return trader.BacktestRun{}, err
	}

	meta.Strategy = strat.Name()
	return executeCandleStrategy(ctx, opts, strat, meta, acct)
}

func collectConfigPaths(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat config path %q: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	matches, err := filepath.Glob(filepath.Join(path, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("glob config path %q: %w", path, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("config directory %q contains no *.yml files", path)
	}

	sort.Strings(matches)
	return matches, nil
}
