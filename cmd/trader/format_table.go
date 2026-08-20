package main

import (
	"fmt"
	"io"

	"github.com/rustyeddy/trader/marketdata"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

// tableFormatter is the default, human-readable Formatter: a plain
// text line per record, the same shape #109/#110 printed directly
// before this issue introduced the Formatter boundary. It never
// returns a non-nil error: every write goes to an already-open
// io.Writer (normally the command's own stdout) with no realistic
// failure mode of its own, the same convention the pre-#111 print
// functions already used.
type tableFormatter struct{}

func (tableFormatter) FormatBars(w io.Writer, resp svc.BarsResponse) error {
	for _, b := range resp.Bars {
		_, _ = fmt.Fprintf(w, "%s  O=%s H=%s L=%s C=%s\n",
			b.Time.Format("2006-01-02T15:04:05Z07:00"), b.Open, b.High, b.Low, b.Close)
	}
	return nil
}

func (tableFormatter) FormatCoverage(w io.Writer, resp svc.CoverageResponse) error {
	for _, p := range resp.Coverage.Partitions {
		_, _ = fmt.Fprintf(w, "%04d-%02d  %s\n", p.Year, int(p.Month), p.Status)
	}
	for _, g := range resp.Coverage.Gaps {
		_, _ = fmt.Fprintf(w, "gap  %s  [%s, %s)\n", g.State, g.Span.Start(), g.Span.End())
	}
	return nil
}

func (tableFormatter) FormatPlan(w io.Writer, resp svc.PlanResponse) error {
	formatTablePlan(w, resp.Plan)
	return nil
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

func (tableFormatter) FormatSync(w io.Writer, resp svc.SyncResponse) error {
	formatTableSyncResult(w, resp.Result)
	return nil
}

func (tableFormatter) FormatBuild(w io.Writer, resp svc.BuildResponse) error {
	formatTableBuildResult(w, resp.Result)
	return nil
}

func (t tableFormatter) FormatUpdateProgress(w io.Writer, resp svc.UpdateResponse) error {
	if resp.SyncPerformed {
		formatTableSyncResult(w, resp.Sync.Result)
	}
	formatTableBuildResult(w, resp.Build.Result)
	return nil
}

func (t tableFormatter) FormatUpdate(w io.Writer, resp svc.UpdateResponse) error {
	_ = t.FormatUpdateProgress(w, resp)
	if !resp.SyncPerformed && len(resp.Build.Result.Published) == 0 && len(resp.Build.Result.Skipped) == 0 {
		_, _ = fmt.Fprintln(w, "already current")
	}
	return nil
}
