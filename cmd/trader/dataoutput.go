package main

import (
	"fmt"
	"io"

	"github.com/rustyeddy/trader/marketdata"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

// printBars, printCoverage, and printPlan are the read commands'
// default output today: a plain, human-readable line per record. They
// are deliberately minimal, not the formatter boundary issue #111
// owns — that issue adds --format table/json and a real presentation
// layer; this is only enough to make issue #109's commands usable and
// testable in the meantime, replaced (not extended) when #111 lands.
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
