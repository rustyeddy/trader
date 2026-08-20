package main

import (
	"fmt"
	"io"

	"github.com/rustyeddy/trader/marketdata"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

// printBars, printCoverage, printPlan, printSyncResult, printBuildResult,
// and printUpdateResponse are every data command's default output
// today: a plain, human-readable line per record. They are
// deliberately minimal, not the formatter boundary issue #111 owns —
// that issue adds --format table/json and a real presentation layer;
// this is only enough to make these commands usable and testable in
// the meantime, replaced (not extended) when #111 lands.
func printBars(w io.Writer, resp svc.BarsResponse) {
	for _, b := range resp.Bars {
		_, _ = fmt.Fprintf(w, "%s  O=%s H=%s L=%s C=%s\n",
			b.Time.Format("2006-01-02T15:04:05Z07:00"), b.Open, b.High, b.Low, b.Close)
	}
}

func printCoverage(w io.Writer, cov marketdata.Coverage) {
	for _, p := range cov.Partitions {
		_, _ = fmt.Fprintf(w, "%04d-%02d  %s\n", p.Year, int(p.Month), p.Status)
	}
	for _, g := range cov.Gaps {
		_, _ = fmt.Fprintf(w, "gap  %s  [%s, %s)\n", g.State, g.Span.Start(), g.Span.End())
	}
}

func printPlan(w io.Writer, plan marketdata.Plan) {
	if len(plan.Actions) == 0 {
		_, _ = fmt.Fprintln(w, "nothing required")
		return
	}
	for _, a := range plan.Actions {
		_, _ = fmt.Fprintf(w, "%s  %s %04d-%02d  %s\n", a.Kind, a.Interval, a.Year, int(a.Month), a.Reason)
	}
}

func printSkipped(w io.Writer, skipped []marketdata.SkippedAction) {
	for _, s := range skipped {
		_, _ = fmt.Fprintf(w, "skipped  %s  %s %04d-%02d  %s\n",
			s.Action.Kind, s.Action.Interval, s.Action.Year, int(s.Action.Month), s.Reason)
	}
}

func printSyncResult(w io.Writer, result marketdata.SyncResult) {
	for _, d := range result.Downloaded {
		_, _ = fmt.Fprintf(w, "downloaded  %s %04d-%02d  %d record(s)\n",
			d.Action.Interval, d.Action.Year, int(d.Action.Month), d.RecordsWritten)
	}
	printSkipped(w, result.Skipped)
}

func printBuildResult(w io.Writer, result marketdata.BuildResult) {
	for _, p := range result.Published {
		_, _ = fmt.Fprintf(w, "published  %s %04d-%02d  %d bar(s)\n",
			p.Action.Interval, p.Action.Year, int(p.Action.Month), p.BarCount)
	}
	printSkipped(w, result.Skipped)
}

// printUpdateProgress prints whatever partial Sync/Build progress resp
// carries, without ever claiming completeness. It is safe to call
// after Update returns an error (dataupdate.go's own error branch
// does this): resp may be a zero or partially-populated value in that
// case (a config/Plan failure never even reaches Sync or Build), and
// this function never prints anything beyond the Downloaded/
// Published/Skipped entries actually present.
func printUpdateProgress(w io.Writer, resp svc.UpdateResponse) {
	if resp.SyncPerformed {
		printSyncResult(w, resp.Sync.Result)
	}
	printBuildResult(w, resp.Build.Result)
}

// printUpdateResponse is printUpdateProgress plus a success-only
// "already current" line, and must therefore only ever be called on
// Update's success path: printing "already current" alongside an
// error would misleadingly suggest the dataset is actually current
// when Update may have failed for an unrelated reason (a config or
// Plan failure, say) before ever reaching Sync or Build.
func printUpdateResponse(w io.Writer, resp svc.UpdateResponse) {
	printUpdateProgress(w, resp)
	if !resp.SyncPerformed && len(resp.Build.Result.Published) == 0 && len(resp.Build.Result.Skipped) == 0 {
		_, _ = fmt.Fprintln(w, "already current")
	}
}
