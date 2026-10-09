package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/rustyeddy/trader/internal/journal"
	"github.com/rustyeddy/trader/internal/report"
)

// signalCounter counts the signal records a run emits, forwarding every
// record to inner when there is one. A model run keeps no trade report, so
// the count is how the summary shows the strategy did something even when
// no --journal was asked for.
type signalCounter struct {
	inner   journal.Recorder
	signals int
}

func (c *signalCounter) Record(ctx context.Context, rec journal.Record) error {
	if rec.Kind == journal.KindSignal {
		c.signals++
	}
	if c.inner == nil {
		return nil
	}
	return c.inner.Record(ctx, rec)
}

func (c *signalCounter) Close() error {
	if c.inner == nil {
		return nil
	}
	return c.inner.Close()
}

// studySummary is what a model (study) run prints instead of the backtest
// report: it trades nothing, so performance and trade statistics would be
// all zeros (issue #489).
type studySummary struct {
	Strategy    string    `json:"strategy"`
	Version     string    `json:"version,omitempty"`
	RunID       string    `json:"run_id"`
	SpanStart   time.Time `json:"span_start"`
	SpanEnd     time.Time `json:"span_end"`
	Interval    string    `json:"interval,omitempty"`
	Instruments int       `json:"instruments"`
	Signals     int       `json:"signals"`
	Journal     string    `json:"journal,omitempty"`
}

func newStudySummary(rep report.BacktestReport, signals int, journalPath string) studySummary {
	instruments := map[string]bool{}
	intervals := map[string]bool{}
	for _, d := range rep.Dataset {
		instruments[d.Instrument] = true
		intervals[d.Interval] = true
	}
	var interval string
	if len(intervals) == 1 {
		for i := range intervals {
			interval = i
		}
	} else if len(intervals) > 1 {
		names := make([]string, 0, len(intervals))
		for i := range intervals {
			names = append(names, i)
		}
		sort.Strings(names)
		interval = fmt.Sprint(names)
	}
	return studySummary{
		Strategy: rep.Run.StrategyName, Version: rep.Run.StrategyVersion, RunID: rep.Run.RunID,
		SpanStart: rep.Run.SpanStart, SpanEnd: rep.Run.SpanEnd,
		Interval: interval, Instruments: len(instruments), Signals: signals, Journal: journalPath,
	}
}

func renderStudy(w io.Writer, format string, sum studySummary) error {
	switch format {
	case formatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(sum)
	case formatTable, formatOrg, "":
		name := sum.Strategy
		if sum.Version != "" {
			name += " " + sum.Version
		}
		fmt.Fprintf(w, "Study: %s\n", name)
		fmt.Fprintf(w, "  Run:          %s\n", sum.RunID)
		fmt.Fprintf(w, "  Span:         %s to %s\n", sum.SpanStart.UTC().Format("2006-01-02"), sum.SpanEnd.UTC().Format("2006-01-02"))
		if sum.Interval != "" {
			fmt.Fprintf(w, "  Interval:     %s\n", sum.Interval)
		}
		fmt.Fprintf(w, "  Instruments:  %d\n", sum.Instruments)
		if sum.Journal != "" {
			fmt.Fprintf(w, "  Signals:      %d (journaled to %s)\n", sum.Signals, sum.Journal)
		} else {
			fmt.Fprintf(w, "  Signals:      %d (not kept; pass --journal PATH to keep them)\n", sum.Signals)
		}
		return nil
	default:
		return fmt.Errorf("invalid --format %q: expected one of %s, %s, %s", format, formatTable, formatJSON, formatOrg)
	}
}
