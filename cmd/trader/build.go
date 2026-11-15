package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/service"
	"github.com/rustyeddy/trader/store"
)

// buildFlags are the options of `trader data build`: the same as download's that
// apply (there is no token and no host, since nothing is fetched).
type buildFlags struct {
	config      string
	dataRoot    string
	instruments []string
	interval    string
	from, to    string
}

func newBuildCmd(a *app) *cobra.Command {
	var f buildFlags
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build canonical bars from the raw store",
		Long: `Build canonical-v2 bars (ADR-090) from the raw OANDA store, one month
partition file at a time:

  <data root>/oanda/canonical/<instrument>/<YYYY>/<MM>/<instrument>-<YYYY>-<MM>-<interval>.csv

Each bar gets its mid price and its modeled availability time. A raw row that is
not a good bar (still forming, off the interval boundary, crossed, malformed in
shape, a duplicate or out of order) is kept in the file as a bad record with its
reason and raw row number, so a partition with bad records is never mistaken for
complete data. A month whose raw file is missing is reported and skipped; a month
that cannot be built is reported as failed and the rest carry on. A file already
built from the same raw bytes is left alone; one from a different build is
replaced. Raw is only read. The command needs no token and makes no network call.
The data root is --data-root, then ` + envDataRoot + `, then marketdata.root in
` + defaultConfigPath + `, then the default ` + store.DefaultRoot + `; --instrument and
--interval also fall back to that file.
h1 and d1 only.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runBuild(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.config, "config", defaultConfigPath, "config file (a missing default file is fine when flags or the environment supply the values)")
	fl.StringVar(&f.dataRoot, "data-root", "", "data root (default: "+envDataRoot+", then marketdata.root in the config file, then "+store.DefaultRoot+")")
	fl.StringSliceVar(&f.instruments, "instrument", nil, "instrument, such as EURUSD; repeat or separate with commas (default: marketdata.instruments)")
	fl.StringVar(&f.interval, "interval", "", "h1 or d1 (default: marketdata.interval)")
	fl.StringVar(&f.from, "from", "", "first instant, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.StringVar(&f.to, "to", "", "end of the range, exclusive, UTC: YYYY-MM-DD or RFC 3339 (required)")
	return cmd
}

// rangeOptions are the options build and status share: what to read, and the data
// root, resolved from flag, environment and file.
type rangeOptions struct {
	symbols  []string
	interval string
	from, to time.Time
}

// resolveRange resolves the shared options (flag, environment, file) and points
// the store at the configured data root. It reads no token and makes no request.
func (a *app) resolveRange(cmd *cobra.Command, f buildFlags) (rangeOptions, error) {
	fl := cmd.Flags()
	cfg, err := loadConfig(f.config, fl.Changed("config"))
	if err != nil {
		return rangeOptions{}, err
	}
	md := cfg.Marketdata
	if md.Provider != "" && md.Provider != "oanda" {
		return rangeOptions{}, fmt.Errorf("config: marketdata.provider %q is not supported (only oanda)", md.Provider)
	}
	if err := useRoot(resolveRoot(f.dataRoot, a.getenv(envDataRoot), md.Root)); err != nil {
		return rangeOptions{}, err
	}
	symbols := pick(fl.Changed("instrument"), f.instruments, md.Instruments)
	if len(symbols) == 0 {
		return rangeOptions{}, errors.New("no instrument: set --instrument or marketdata.instruments in " + defaultConfigPath)
	}
	interval := strings.ToLower(pickString(fl.Changed("interval"), f.interval, md.Interval))
	if interval == "" {
		return rangeOptions{}, errors.New("no interval: set --interval or marketdata.interval in " + defaultConfigPath)
	}
	from, to, err := parseRange(f.from, f.to)
	if err != nil {
		return rangeOptions{}, err
	}
	return rangeOptions{symbols: symbols, interval: interval, from: from, to: to}, nil
}

// runBuild resolves every option and hands the request to the application
// service. It is transport only: no provider and no domain package is imported
// here.
func (a *app) runBuild(cmd *cobra.Command, f buildFlags) error {
	opts, err := a.resolveRange(cmd, f)
	if err != nil {
		return err
	}
	symbols, interval, from, to := opts.symbols, opts.interval, opts.from, opts.to

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	out := cmd.OutOrStdout()
	var writeErr error // the first failure to write the summary
	say := func(format string, args ...any) {
		if _, werr := fmt.Fprintf(out, format, args...); werr != nil && writeErr == nil {
			writeErr = fmt.Errorf("write summary: %w", werr)
		}
	}
	var counts struct {
		built, unchanged, replaced, missing, failed int
		good, bad                                   int
		reported                                    int // partitions reported so far
	}
	req := service.BuildRequest{Instruments: symbols, Interval: interval, From: from, To: to, BuiltAt: time.Now().UTC()}
	err = service.BuildCanonical(ctx, logging.WithComponent(slog.Default(), logging.ComponentMarketData), req, func(r service.PartitionResult) {
		counts.reported++
		counts.good += r.Good
		counts.bad += r.Bad
		switch r.Action {
		case service.Built:
			counts.built++
		case service.Unchanged:
			counts.unchanged++
		case service.Replaced:
			counts.replaced++
		case service.Missing:
			counts.missing++
		case service.Failed:
			counts.failed++
		}
		name := fmt.Sprintf("%s %04d-%02d", r.Instrument, r.Year, int(r.Month))
		switch r.Action {
		case service.Missing:
			say("%s: no raw partition\n", name)
		case service.Failed:
			say("%s: failed: %v\n", name, r.Err)
		default:
			say("%s: %s, good %d, bad %d\n", filepath.Join(store.Root(), r.Path), r.Action, r.Good, r.Bad)
		}
	})
	if err == nil || counts.reported > 0 { // a request that could not run has no total
		say("total: built %d, unchanged %d, replaced %d, missing raw %d, failed %d; good bars %d, bad records %d\n",
			counts.built, counts.unchanged, counts.replaced, counts.missing, counts.failed, counts.good, counts.bad)
	}
	return errors.Join(err, writeErr)
}
