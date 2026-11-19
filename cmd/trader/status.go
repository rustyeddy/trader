package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/service"
)

// statusFlags are the options of `trader data status`: build's, plus the two
// switches that accept a month that is missing or has bad records.
type statusFlags struct {
	buildFlags
	allowBad, allowMissing bool
}

func newStatusCmd(a *app) *cobra.Command {
	var f statusFlags
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show which canonical months can be loaded",
		Long: `Check the canonical bars (ADR-090) a range would load, without changing anything.
For each instrument it reads every UTC month the range touches and says what it
found:

  loaded   a valid file from the build in force, with no bad records
  bad      the file has bad records, so it is not complete data
  missing  there is no canonical file for the month
  stale    the file is not from the build in force (another availability policy,
           builder or validator version) or is not canonical-v2: it is never used;
           rebuild it with trader data build

By default the command fails if any month is stale, missing or bad. --allow-bad and
--allow-missing accept those two (a stale month is never accepted). It reads only,
needs no token and makes no network call. The data root is --data-root, then
` + envDataRoot + `, then marketdata.root in ` + defaultConfigPath + `, then the default ` + store.DefaultRoot + `;
--instrument and --interval also fall back to that file. h1 and d1 only.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runStatus(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.config, "config", defaultConfigPath, "config file (a missing default file is fine when flags or the environment supply the values)")
	fl.StringVar(&f.dataRoot, "data-root", "", "data root (default: "+envDataRoot+", then marketdata.root in the config file, then "+store.DefaultRoot+")")
	fl.StringSliceVar(&f.instruments, "instrument", nil, "instrument, such as EURUSD; repeat or separate with commas (default: marketdata.instruments)")
	fl.StringVar(&f.interval, "interval", "", "h1 or d1 (default: marketdata.interval)")
	fl.StringVar(&f.from, "from", "", "first instant, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.StringVar(&f.to, "to", "", "end of the range, exclusive, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.BoolVar(&f.allowBad, "allow-bad", false, "accept months that have bad records (their good bars load)")
	fl.BoolVar(&f.allowMissing, "allow-missing", false, "accept months with no canonical file (a gap in the series)")
	return cmd
}

// runStatus resolves the options (flag, environment, file) and hands the request
// to the application service. Transport only: no provider and no domain package.
func (a *app) runStatus(cmd *cobra.Command, f statusFlags) error {
	opts, err := a.resolveRange(cmd, f.buildFlags)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	out := cmd.OutOrStdout()
	var writeErr error // the first failure to write the summary
	say := func(format string, args ...any) {
		if _, werr := fmt.Fprintf(out, format, args...); werr != nil && writeErr == nil {
			writeErr = fmt.Errorf("write summary: %w", werr)
		}
	}
	req := service.LoadRequest{Instruments: opts.symbols, Interval: opts.interval, From: opts.from, To: opts.to,
		AllowBad: f.allowBad, AllowMissing: f.allowMissing}
	results, err := service.LoadSeries(ctx, logging.WithComponent(slog.Default(), logging.ComponentMarketData), req)

	var loaded, bad, missing, stale, bars, closures, holes, unexplained int
	for _, r := range results {
		for _, m := range r.Months {
			name := fmt.Sprintf("%s %04d-%02d", r.Instrument, m.Year, int(m.Month))
			switch m.State {
			case service.MonthLoaded:
				loaded++
				say("%s: loaded, good %d\n", name, m.Good)
			case service.MonthBad:
				bad++
				say("%s: bad, good %d, bad %d\n", name, m.Good, m.Bad)
			case service.MonthMissing:
				missing++
				say("%s: missing\n", name)
			case service.MonthStale:
				stale++
				say("%s: stale: %s\n", name, m.Reason)
			}
		}
		for _, g := range r.Gaps {
			say("%s: gap %s\n", r.Instrument, g)
			switch g.Kind {
			case service.GapUnexplained:
				unexplained++
			case service.GapKnownHole:
				holes++
			default:
				closures++
			}
		}
		if r.Err != nil {
			say("%s: not loadable\n", r.Instrument)
			continue
		}
		bars += len(r.Series.Bars)
		say("%s: %d bars\n", r.Instrument, len(r.Series.Bars))
	}
	if len(results) > 0 {
		say("total: loaded %d, bad %d, missing %d, stale %d; bars %d; gaps: %d unexplained, %d known closures, %d known holes\n",
			loaded, bad, missing, stale, bars, unexplained, closures, holes)
	}
	return errors.Join(err, writeErr)
}
