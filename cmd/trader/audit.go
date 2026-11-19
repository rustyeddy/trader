package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/service"
)

const (
	formatText = "text"
	formatJSON = "json"
)

type auditFlags struct {
	buildFlags
	allowBad, allowMissing bool
	sample                 int
	format                 string
}

func newAuditCmd(a *app) *cobra.Command {
	var f auditFlags
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit canonical data: months, gaps and the raw cross-check",
		Long: `Audit the canonical bars (ADR-090) of a range in one report, changing nothing.
For each instrument it combines:

  months   loaded, bad, missing or stale (a file from another build)
  gaps     bars missing inside the series against the FX calendar: weekends are not
           reported, year-end holidays are known closures, anything else is
           unexplained
  raw      each canonical file against the raw partition it was built from (the
           checks of trader data verify): raw rows accounted for, exact prices and
           volume, bad-record reasons, the raw fingerprint and build, and canonical
           validity (duplicate or out-of-order times, invalid OHLC)

The command exits non-zero if the data is not sound: a stale month, a bad or
missing month or an unexplained gap (unless --allow-bad / --allow-missing accept
them, as they do for status), or a file that disagrees with its raw. --format json
prints the whole report as one JSON object for tools; the default is text. --sample N
limits the raw cross-check to N months per instrument; months and gaps always cover
the whole range. h1 and d1 only; OANDA only. It reads only and needs no token or
network. The data root is --data-root, then ` + envDataRoot + `, then marketdata.root in
` + defaultConfigPath + `, then the default ` + store.DefaultRoot + `.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runAudit(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.config, "config", defaultConfigPath, "config file (a missing default file is fine when flags or the environment supply the values)")
	fl.StringVar(&f.dataRoot, "data-root", "", "data root (default: "+envDataRoot+", then marketdata.root in the config file, then "+store.DefaultRoot+")")
	fl.StringSliceVar(&f.instruments, "instrument", nil, "instrument, such as EURUSD; repeat or separate with commas (default: marketdata.instruments)")
	fl.StringVar(&f.interval, "interval", "", "h1 or d1 (default: marketdata.interval)")
	fl.StringVar(&f.from, "from", "", "first instant, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.StringVar(&f.to, "to", "", "end of the range, exclusive, UTC: YYYY-MM-DD or RFC 3339 (required)")
	fl.BoolVar(&f.allowBad, "allow-bad", false, "accept months that have bad records")
	fl.BoolVar(&f.allowMissing, "allow-missing", false, "accept months with no canonical file and gaps the calendar does not explain")
	fl.IntVar(&f.sample, "sample", 0, "cross-check only N months per instrument against raw, evenly spaced (0: every month)")
	fl.StringVar(&f.format, "format", formatText, "output format: text or json")
	return cmd
}

// runAudit resolves the options and hands the request to the application service,
// then formats the report. Transport only: no provider and no domain package.
func (a *app) runAudit(cmd *cobra.Command, f auditFlags) error {
	if f.format != formatText && f.format != formatJSON {
		return fmt.Errorf("--format %q: want %s or %s", f.format, formatText, formatJSON)
	}
	opts, err := a.resolveRange(cmd, f.buildFlags)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	req := service.AuditRequest{Instruments: opts.symbols, Interval: opts.interval, From: opts.from, To: opts.to,
		AllowBad: f.allowBad, AllowMissing: f.allowMissing, Sample: f.sample}
	report, err := service.Audit(ctx, logging.WithComponent(slog.Default(), logging.ComponentMarketData), req)
	if err != nil && !errors.Is(err, service.ErrAuditFailed) {
		return err // the request could not run, or was cancelled: there is no report
	}
	out := cmd.OutOrStdout()
	if f.format == formatJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if werr := enc.Encode(report); werr != nil {
			return errors.Join(err, fmt.Errorf("write report: %w", werr))
		}
		return err
	}
	var writeErr error // the first failure to write the report
	say := func(format string, args ...any) {
		if _, werr := fmt.Fprintf(out, format, args...); werr != nil && writeErr == nil {
			writeErr = fmt.Errorf("write report: %w", werr)
		}
	}
	writeAuditText(say, report)
	return errors.Join(err, writeErr)
}

func writeAuditText(say func(string, ...any), r service.AuditReport) {
	for _, ia := range r.Instruments {
		say("%s\n", ia.Instrument)
		for _, m := range ia.Months {
			line := fmt.Sprintf("  %04d-%02d: %s", m.Year, m.Month, m.State)
			switch m.State {
			case "loaded":
				line += fmt.Sprintf(", good %d", m.Good)
			case "bad":
				line += fmt.Sprintf(", good %d, bad %d", m.Good, m.Bad)
			case "stale":
				line += ": " + m.Reason
			}
			say("%s\n", line)
		}
		for _, g := range ia.Gaps {
			say("  gap %s %s %s, %d bars\n", g.Kind, g.Start.Format(time.RFC3339), g.End.Format(time.RFC3339), g.Bars)
		}
		for _, p := range ia.Raw {
			name := fmt.Sprintf("  raw %04d-%02d", p.Year, p.Month)
			switch p.State {
			case "verified":
				say("%s: verified, rows %d, bars %d, bad %d%s\n", name, p.RawRows, p.Bars, p.Bad, explainedText(p.Explained))
			case service.AuditUnverifiable:
				say("%s: UNVERIFIABLE, a canonical file and no raw partition to check it against\n", name)
			case "no raw":
				say("%s: no raw partition\n", name)
			case "no canonical":
				say("%s: rows %d, no canonical file\n", name, p.RawRows)
			default:
				if p.Error != "" {
					say("%s: failed: %s\n", name, p.Error)
					continue
				}
				say("%s: FAILED, rows %d, bars %d, bad %d, %d findings\n", name, p.RawRows, p.Bars, p.Bad, len(p.Findings)+p.Omitted)
				for _, fd := range p.Findings {
					say("    %s: %s\n", fd.Kind, auditFindingText(fd))
				}
				if p.Omitted > 0 {
					say("    and %d more\n", p.Omitted)
				}
			}
		}
		if ia.LoadError != "" {
			say("  not loadable: %s\n", ia.LoadError)
		}
		say("  %s: %d bars\n", okText(ia.OK), ia.Bars)
	}
	s := r.Summary
	say("total: months %d (loaded %d, bad %d, missing %d, stale %d); bars %d; gaps %d unexplained, %d known closures, %d known holes; raw verified %d, failed %d, unverifiable %d, no raw %d, no canonical %d, findings %d\n",
		s.Months, s.MonthsLoaded, s.MonthsBad, s.MonthsMissing, s.MonthsStale, s.Bars, s.GapsUnexplained, s.GapsClosure, s.GapsKnownHole,
		s.RawVerified, s.RawFailed, s.RawUnverifiable, s.RawNoRaw, s.RawNoCanonical, s.RawFindings)
	if r.Sample > 0 {
		// Months and gaps were checked over the whole range; the raw cross-check
		// only over a sample, so an OK certifies only that part.
		say("note: sampled: the raw cross-check covered %d of %d months\n", s.RawChecked, s.Months)
	}
	say("audit: %s\n", okText(r.OK))
}

func okText(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAILED"
}

func auditFindingText(f service.AuditFinding) string {
	s := f.Detail
	if f.RawRow > 0 {
		s = fmt.Sprintf("row %d: %s", f.RawRow, s)
	}
	if f.Time != nil {
		s = f.Time.Format("2006-01-02T15:04Z") + " " + s
	}
	return s
}
