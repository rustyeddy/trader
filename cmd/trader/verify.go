package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/service"
)

type verifyFlags struct {
	buildFlags
	sample int
}

func newVerifyCmd(a *app) *cobra.Command {
	var f verifyFlags
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Check canonical bars against the raw store",
		Long: `Check each canonical file (ADR-090) against the raw partition it was built from,
without changing anything. For every month of the range it asks, row by row:

  - is every raw row either the source of one bar or a bad record, and does every
    bar and bad record have its raw row?
  - do a bar's bid, ask and volume equal its raw row's exactly (prices are exact
    decimals; nothing is rounded or tolerated), and is its mid their midpoint?
  - is each bad record's reason true of its raw row (still forming, off the
    boundary, a repeat, out of order, crossed, invalid)? These are the only expected
    differences between raw and canonical; they are counted, not failures.
  - was the file built from the raw bytes now on disk, by the builder, validator and
    availability policy in force, and is it valid canonical-v2 (times ascending, so
    no duplicates; on the boundary; valid OHLC)?

A month with no raw, or raw and no canonical file, is reported and is not a
failure. The command exits non-zero if any month disagrees. --sample N checks N
months per instrument, evenly spaced from the first to the last of the range (each
in full); without it every month is checked. h1 and d1 only; OANDA only. It reads
only and needs no token or network. The data root is --data-root, then
` + envDataRoot + `, then marketdata.root in ` + defaultConfigPath + `, then the default ` + store.DefaultRoot + `.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runVerify(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.config, "config", defaultConfigPath, "config file (a missing default file is fine when flags or the environment supply the values)")
	fl.StringVar(&f.dataRoot, "data-root", "", "data root (default: "+envDataRoot+", then marketdata.root in the config file, then "+store.DefaultRoot+")")
	fl.StringSliceVar(&f.instruments, "instrument", nil, "instrument, such as EURUSD; repeat or separate with commas (default: marketdata.instruments)")
	fl.StringVar(&f.interval, "interval", "", "h1 or d1 (default: marketdata.interval)")
	fl.StringVar(&f.from, "from", "", "first instant, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.StringVar(&f.to, "to", "", "end of the range, exclusive, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.IntVar(&f.sample, "sample", 0, "check only N months per instrument, evenly spaced (0: every month)")
	return cmd
}

// runVerify resolves the options and hands the request to the application
// service. Transport only: no provider and no domain package.
func (a *app) runVerify(cmd *cobra.Command, f verifyFlags) error {
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
	var counts struct {
		verified, failed, noRaw, noCanonical int
		findings, rawRows                    int
		reported                             int
	}
	explained := map[string]int{}
	req := service.VerifyRequest{Instruments: opts.symbols, Interval: opts.interval, From: opts.from, To: opts.to, Sample: f.sample}
	err = service.VerifyCanonical(ctx, logging.WithComponent(slog.Default(), logging.ComponentMarketData), req, func(r service.PartitionVerification) {
		counts.reported++
		counts.rawRows += r.RawRows
		name := fmt.Sprintf("%s %04d-%02d", r.Instrument, r.Year, int(r.Month))
		for reason, n := range r.Explained {
			explained[reason] += n
		}
		switch r.State {
		case service.VerifyOK:
			counts.verified++
			say("%s: verified, raw rows %d, bars %d, bad %d%s\n", filepath.Join(store.Root(), r.Path), r.RawRows, r.Bars, r.Bad, explainedText(r.Explained))
		case service.VerifyNoRaw:
			counts.noRaw++
			say("%s: no raw partition\n", name)
		case service.VerifyNoCanonical:
			counts.noCanonical++
			say("%s: raw rows %d, no canonical file\n", name, r.RawRows)
		default:
			counts.failed++
			if r.Err != nil {
				say("%s: failed: %v\n", name, r.Err)
				return
			}
			say("%s: FAILED, raw rows %d, bars %d, bad %d, %d findings\n", filepath.Join(store.Root(), r.Path), r.RawRows, r.Bars, r.Bad, len(r.Findings)+r.Omitted)
			for _, fd := range r.Findings {
				say("  %s\n", fd)
			}
			if r.Omitted > 0 {
				say("  and %d more\n", r.Omitted)
			}
			counts.findings += len(r.Findings) + r.Omitted
		}
	})
	if err == nil || counts.reported > 0 { // a request that could not run has no total
		say("total: verified %d, failed %d, no raw %d, no canonical %d; raw rows %d, findings %d%s\n",
			counts.verified, counts.failed, counts.noRaw, counts.noCanonical, counts.rawRows, counts.findings, explainedText(explained))
	}
	return errors.Join(err, writeErr)
}

// explainedText renders the expected differences, " (explained: incomplete 2, ...)",
// or nothing when there are none.
func explainedText(explained map[string]int) string {
	if len(explained) == 0 {
		return ""
	}
	reasons := make([]string, 0, len(explained))
	for reason := range explained {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		parts = append(parts, fmt.Sprintf("%s %d", reason, explained[reason]))
	}
	return " (explained: " + strings.Join(parts, ", ") + ")"
}
