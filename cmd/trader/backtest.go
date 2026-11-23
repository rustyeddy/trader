package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/service"
)

// backtestFlags are the options of `trader backtest`: status's, plus the source.
type backtestFlags struct {
	statusFlags
	source      string
	resultsRoot string
}

func newBacktestCmd(a *app) *cobra.Command {
	var f backtestFlags
	cmd := &cobra.Command{
		Use:   "backtest",
		Short: "Backtest canonical bars from the data store",
		Long: `Backtest the canonical bars (ADR-090) of one or more instruments over a UTC
range, all at one interval and from one source, on one shared account: EMAX on each
instrument, with the fixed settings of the EMAX scenarios. Every bar available at
the same instant reaches the strategy together; an instrument with no bar at that
instant is absent, never carried forward.

Months are loaded as trader data status describes them: by default the run fails
if any month is stale, missing or bad. --allow-bad and --allow-missing accept
those two (a stale month is never accepted). The command reads only, needs no
token and makes no network call. The data root is --data-root, then
` + envDataRoot + `, then marketdata.root in ` + defaultConfigPath + `, then the default ` + store.DefaultRoot + `;
--instrument and --interval also fall back to that file. h1 and d1 only; the
engine trades the instruments it has a market for.

Every run is journaled: it gets a RunID and a directory of that name under the
results root, holding run.json (what the run was, and how it ended) and
events.jsonl (every order transition). The results root is --results-root, then
` + envResultsRoot + `, then results.root in ` + defaultConfigPath + `, then the default ` + service.DefaultResultsRoot + `.
A run that fails or is cancelled is journaled too, with its status and why.
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runBacktest(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.config, "config", defaultConfigPath, "config file (a missing default file is fine when flags or the environment supply the values)")
	fl.StringVar(&f.dataRoot, "data-root", "", "data root (default: "+envDataRoot+", then marketdata.root in the config file, then "+store.DefaultRoot+")")
	fl.StringVar(&f.source, "source", "oanda", "the provider the bars came from (only oanda)")
	fl.StringSliceVar(&f.instruments, "instrument", nil, "instrument, such as EURUSD; repeat or separate with commas (default: marketdata.instruments)")
	fl.StringVar(&f.interval, "interval", "", "h1 or d1 (default: marketdata.interval)")
	fl.StringVar(&f.from, "from", "", "first instant, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.StringVar(&f.to, "to", "", "end of the range, exclusive, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.StringVar(&f.resultsRoot, "results-root", "", "results root (default: "+envResultsRoot+", then results.root in the config file, then "+service.DefaultResultsRoot+")")
	fl.BoolVar(&f.allowBad, "allow-bad", false, "accept months that have bad records (their good bars load)")
	fl.BoolVar(&f.allowMissing, "allow-missing", false, "accept months with no canonical file (a gap in the series)")
	return cmd
}

// runBacktest resolves the options (flag, environment, file), hands the request to
// the application service and prints what it returns. Transport only.
func (a *app) runBacktest(cmd *cobra.Command, f backtestFlags) error {
	opts, err := a.resolveRange(cmd, f.buildFlags)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	res, err := service.RunBacktest(ctx, logging.WithComponent(slog.Default(), logging.ComponentMarketData), service.BacktestRequest{
		Source: f.source, Instruments: opts.symbols, Interval: opts.interval, From: opts.from, To: opts.to,
		AllowBad: f.allowBad, AllowMissing: f.allowMissing,
		ResultsRoot: orDefault(resolveRoot(f.resultsRoot, a.getenv(envResultsRoot), opts.resultsRoot), service.DefaultResultsRoot),
	})
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if err := res.Write(out); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	if _, err := fmt.Fprintf(out, "run %s: %s\n", res.RunID, res.Dir); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

// orDefault is v, or def when v is empty.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
