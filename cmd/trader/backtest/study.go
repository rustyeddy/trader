package backtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
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
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(sum); err != nil {
			return err
		}
		_, err := w.Write(buf.Bytes())
		return err
	case formatTable, "":
		return writeString(w, studyTable(sum))
	case formatOrg:
		return writeString(w, studyOrg(sum))
	default:
		return fmt.Errorf("invalid --format %q: expected one of %s, %s, %s", format, formatTable, formatJSON, formatOrg)
	}
}

// writeString writes s with a single call so a failed write is reported
// instead of leaving a silently truncated summary.
func writeString(w io.Writer, s string) error {
	_, err := io.WriteString(w, s)
	return err
}

func (s studySummary) title() string {
	name := s.Strategy
	if s.Version != "" {
		name += " " + s.Version
	}
	return name
}

func (s studySummary) signalsNote() string {
	if s.Journal != "" {
		return fmt.Sprintf("%d (journaled to %s)", s.Signals, s.Journal)
	}
	return fmt.Sprintf("%d (not kept; pass --journal PATH to keep them)", s.Signals)
}

func (s studySummary) span() (start, end string) {
	return s.SpanStart.UTC().Format(time.RFC3339), s.SpanEnd.UTC().Format(time.RFC3339)
}

func studyTable(sum studySummary) string {
	start, end := sum.span()
	var b strings.Builder
	fmt.Fprintf(&b, "Study: %s\n", sum.title())
	fmt.Fprintf(&b, "  Run:          %s\n", sum.RunID)
	fmt.Fprintf(&b, "  Span:         %s to %s\n", start, end)
	if sum.Interval != "" {
		fmt.Fprintf(&b, "  Interval:     %s\n", sum.Interval)
	}
	fmt.Fprintf(&b, "  Instruments:  %d\n", sum.Instruments)
	fmt.Fprintf(&b, "  Signals:      %s\n", sum.signalsNote())
	return b.String()
}

// studyOrg mirrors the structure of report.OrgRenderer: a title, a
// properties drawer with the run identity, then the body as a table.
func studyOrg(sum studySummary) string {
	start, end := sum.span()
	var b strings.Builder
	fmt.Fprintf(&b, "#+TITLE: Study: %s\n", sum.title())
	b.WriteString(":PROPERTIES:\n")
	fmt.Fprintf(&b, ":RUN_ID: %s\n", sum.RunID)
	fmt.Fprintf(&b, ":SPAN_START: %s\n", start)
	fmt.Fprintf(&b, ":SPAN_END: %s\n", end)
	b.WriteString(":END:\n\n* Summary\n")
	b.WriteString("| Field | Value |\n|-------+-------|\n")
	if sum.Interval != "" {
		fmt.Fprintf(&b, "| Interval | %s |\n", sum.Interval)
	}
	fmt.Fprintf(&b, "| Instruments | %d |\n", sum.Instruments)
	fmt.Fprintf(&b, "| Signals | %s |\n", sum.signalsNote())
	return b.String()
}
