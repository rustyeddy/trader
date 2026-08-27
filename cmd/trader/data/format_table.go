package data

import (
	"fmt"
	"io"

	"github.com/rustyeddy/trader/marketdata"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

// tableFormatter is the default, human-readable Formatter: a plain
// text line per record, the same shape #109/#110 printed directly
// before this issue introduced the Formatter boundary. Unlike an
// earlier version of this file, it does not discard write errors: a
// broken pipe or other output failure (piping trader's stdout into a
// command that exits early, say) is reported through the same error
// return jsonFormatter already used, via errWriter below, rather than
// silently looking like success.
type tableFormatter struct{}

// errWriter wraps an io.Writer and remembers the first error any
// Write call returns, turning every write after that into a no-op.
// This is the standard "sticky error" idiom: every formatTableXxx
// helper below can keep calling fmt.Fprintf without checking each
// individual return value, and the owning FormatXxx method reports
// whatever the first failure was, once, at the end.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	if err != nil {
		e.err = err
	}
	return n, err
}

func (t tableFormatter) FormatBars(w io.Writer, resp svc.BarsResponse) error {
	ew := &errWriter{w: w}
	for _, b := range resp.Bars {
		_, _ = fmt.Fprintf(ew, "%s  O=%s H=%s L=%s C=%s\n",
			b.Time.Format("2006-01-02T15:04:05Z07:00"), b.Open, b.High, b.Low, b.Close)
	}
	return ew.err
}

func (t tableFormatter) FormatCoverage(w io.Writer, resp svc.CoverageResponse) error {
	ew := &errWriter{w: w}
	for _, p := range resp.Coverage.Partitions {
		_, _ = fmt.Fprintf(ew, "%04d-%02d  %s\n", p.Year, int(p.Month), p.Status)
	}
	for _, g := range resp.Coverage.Gaps {
		_, _ = fmt.Fprintf(ew, "gap  %s  [%s, %s)\n", g.State, g.Span.Start(), g.Span.End())
	}
	return ew.err
}

func (t tableFormatter) FormatPlan(w io.Writer, resp svc.PlanResponse) error {
	ew := &errWriter{w: w}
	formatTablePlan(ew, resp.Plan)
	return ew.err
}

func formatTablePlan(w io.Writer, plan marketdata.Plan) {
	if len(plan.Actions) == 0 {
		_, _ = fmt.Fprintln(w, "nothing required")
		return
	}
	for _, a := range plan.Actions {
		_, _ = fmt.Fprintf(w, "%s  %s %04d-%02d  %s\n", a.Kind, a.Interval, a.Year, int(a.Month), a.Reason)
	}
}

func formatTableSkipped(w io.Writer, skipped []marketdata.SkippedAction) {
	for _, s := range skipped {
		_, _ = fmt.Fprintf(w, "skipped  %s  %s %04d-%02d  %s\n",
			s.Action.Kind, s.Action.Interval, s.Action.Year, int(s.Action.Month), s.Reason)
	}
}

func formatTableSyncResult(w io.Writer, result marketdata.SyncResult) {
	for _, d := range result.Downloaded {
		_, _ = fmt.Fprintf(w, "downloaded  %s %04d-%02d  %d record(s)\n",
			d.Action.Interval, d.Action.Year, int(d.Action.Month), d.RecordsWritten)
	}
	formatTableSkipped(w, result.Skipped)
}

func formatTableBuildResult(w io.Writer, result marketdata.BuildResult) {
	for _, p := range result.Published {
		_, _ = fmt.Fprintf(w, "published  %s %04d-%02d  %d bar(s)\n",
			p.Action.Interval, p.Action.Year, int(p.Action.Month), p.BarCount)
	}
	formatTableSkipped(w, result.Skipped)
}

func (t tableFormatter) FormatSync(w io.Writer, resp svc.SyncResponse) error {
	ew := &errWriter{w: w}
	formatTableSyncResult(ew, resp.Result)
	return ew.err
}

func (t tableFormatter) FormatBuild(w io.Writer, resp svc.BuildResponse) error {
	ew := &errWriter{w: w}
	formatTableBuildResult(ew, resp.Result)
	return ew.err
}

func (t tableFormatter) FormatUpdateProgress(w io.Writer, resp svc.UpdateResponse) error {
	ew := &errWriter{w: w}
	if resp.SyncPerformed {
		formatTableSyncResult(ew, resp.Sync.Result)
	}
	formatTableBuildResult(ew, resp.Build.Result)
	return ew.err
}

func (t tableFormatter) FormatUpdate(w io.Writer, resp svc.UpdateResponse) error {
	if err := t.FormatUpdateProgress(w, resp); err != nil {
		return err
	}
	if !resp.SyncPerformed && len(resp.Build.Result.Published) == 0 && len(resp.Build.Result.Skipped) == 0 {
		ew := &errWriter{w: w}
		_, _ = fmt.Fprintln(ew, "already current")
		return ew.err
	}
	return nil
}
