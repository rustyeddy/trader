package main

import (
	"encoding/json"
	"io"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

// jsonFormatter renders a stable, structured JSON document per
// response. Domain types this package's response fields carry --
// instrument.ID, marketdata.Interval, marketdata.TimeRange -- hold
// only unexported fields and no MarshalJSON of their own (by design:
// adding one purely to satisfy this transport's presentation needs is
// exactly the kind of service/domain change issue #111 says not to
// make). Every jsonFormatter method therefore first converts a
// response's domain values into small, unexported view types defined
// in this file, each rendering the same information formatTableXxx's
// text form already shows (via each domain type's own Stringer), and
// encodes those instead of the response value itself.
type jsonFormatter struct{}

func (jsonFormatter) encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type jsonBar struct {
	Time  time.Time `json:"time"`
	Open  num.Price `json:"open"`
	High  num.Price `json:"high"`
	Low   num.Price `json:"low"`
	Close num.Price `json:"close"`
}

func toJSONBars(bars []marketdata.Bar) []jsonBar {
	out := make([]jsonBar, len(bars))
	for i, b := range bars {
		out[i] = jsonBar{Time: b.Time, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close}
	}
	return out
}

func (f jsonFormatter) FormatBars(w io.Writer, resp svc.BarsResponse) error {
	return f.encode(w, struct {
		Bars []jsonBar `json:"bars"`
	}{Bars: toJSONBars(resp.Bars)})
}

type jsonPartitionCoverage struct {
	Year   int    `json:"year"`
	Month  int    `json:"month"`
	Status string `json:"status"`
}

type jsonGap struct {
	State string    `json:"state"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func (f jsonFormatter) FormatCoverage(w io.Writer, resp svc.CoverageResponse) error {
	partitions := make([]jsonPartitionCoverage, len(resp.Coverage.Partitions))
	for i, p := range resp.Coverage.Partitions {
		partitions[i] = jsonPartitionCoverage{Year: p.Year, Month: int(p.Month), Status: p.Status.String()}
	}
	gaps := make([]jsonGap, len(resp.Coverage.Gaps))
	for i, g := range resp.Coverage.Gaps {
		gaps[i] = jsonGap{State: g.State.String(), Start: g.Span.Start(), End: g.Span.End()}
	}
	return f.encode(w, struct {
		Partitions []jsonPartitionCoverage `json:"partitions"`
		Gaps       []jsonGap               `json:"gaps"`
	}{Partitions: partitions, Gaps: gaps})
}

type jsonAction struct {
	Kind     string `json:"kind"`
	Interval string `json:"interval"`
	Year     int    `json:"year"`
	Month    int    `json:"month"`
	Reason   string `json:"reason"`
}

func toJSONActions(actions []marketdata.Action) []jsonAction {
	out := make([]jsonAction, len(actions))
	for i, a := range actions {
		out[i] = jsonAction{
			Kind: a.Kind.String(), Interval: a.Interval.String(),
			Year: a.Year, Month: int(a.Month), Reason: a.Reason,
		}
	}
	return out
}

func (f jsonFormatter) FormatPlan(w io.Writer, resp svc.PlanResponse) error {
	return f.encode(w, struct {
		Actions []jsonAction `json:"actions"`
	}{Actions: toJSONActions(resp.Plan.Actions)})
}

type jsonDownload struct {
	Interval       string `json:"interval"`
	Year           int    `json:"year"`
	Month          int    `json:"month"`
	RecordsWritten int    `json:"records_written"`
}

type jsonSkipped struct {
	Kind     string `json:"kind"`
	Interval string `json:"interval"`
	Year     int    `json:"year"`
	Month    int    `json:"month"`
	Reason   string `json:"reason"`
}

func toJSONSkipped(skipped []marketdata.SkippedAction) []jsonSkipped {
	out := make([]jsonSkipped, len(skipped))
	for i, s := range skipped {
		out[i] = jsonSkipped{
			Kind: s.Action.Kind.String(), Interval: s.Action.Interval.String(),
			Year: s.Action.Year, Month: int(s.Action.Month), Reason: s.Reason,
		}
	}
	return out
}

type jsonSyncResult struct {
	Downloaded []jsonDownload `json:"downloaded"`
	Skipped    []jsonSkipped  `json:"skipped"`
}

func toJSONSyncResult(result marketdata.SyncResult) jsonSyncResult {
	downloaded := make([]jsonDownload, len(result.Downloaded))
	for i, d := range result.Downloaded {
		downloaded[i] = jsonDownload{
			Interval: d.Action.Interval.String(), Year: d.Action.Year,
			Month: int(d.Action.Month), RecordsWritten: d.RecordsWritten,
		}
	}
	return jsonSyncResult{Downloaded: downloaded, Skipped: toJSONSkipped(result.Skipped)}
}

func (f jsonFormatter) FormatSync(w io.Writer, resp svc.SyncResponse) error {
	return f.encode(w, toJSONSyncResult(resp.Result))
}

type jsonPublished struct {
	Interval string `json:"interval"`
	Year     int    `json:"year"`
	Month    int    `json:"month"`
	BarCount int    `json:"bar_count"`
}

type jsonBuildResult struct {
	Published []jsonPublished `json:"published"`
	Skipped   []jsonSkipped   `json:"skipped"`
}

func toJSONBuildResult(result marketdata.BuildResult) jsonBuildResult {
	published := make([]jsonPublished, len(result.Published))
	for i, p := range result.Published {
		published[i] = jsonPublished{
			Interval: p.Action.Interval.String(), Year: p.Action.Year,
			Month: int(p.Action.Month), BarCount: p.BarCount,
		}
	}
	return jsonBuildResult{Published: published, Skipped: toJSONSkipped(result.Skipped)}
}

func (f jsonFormatter) FormatBuild(w io.Writer, resp svc.BuildResponse) error {
	return f.encode(w, toJSONBuildResult(resp.Result))
}

type jsonUpdateResponse struct {
	SyncPerformed  bool            `json:"sync_performed"`
	Sync           *jsonSyncResult `json:"sync,omitempty"`
	Build          jsonBuildResult `json:"build"`
	AlreadyCurrent bool            `json:"already_current"`
}

func toJSONUpdateResponse(resp svc.UpdateResponse, alreadyCurrent bool) jsonUpdateResponse {
	out := jsonUpdateResponse{
		SyncPerformed:  resp.SyncPerformed,
		Build:          toJSONBuildResult(resp.Build.Result),
		AlreadyCurrent: alreadyCurrent,
	}
	if resp.SyncPerformed {
		sync := toJSONSyncResult(resp.Sync.Result)
		out.Sync = &sync
	}
	return out
}

// FormatUpdateProgress always reports already_current: false -- it is
// used only from dataupdate.go's error branch (see Formatter's own
// doc comment), where the dataset's completeness is, by definition,
// unknown or false.
func (f jsonFormatter) FormatUpdateProgress(w io.Writer, resp svc.UpdateResponse) error {
	return f.encode(w, toJSONUpdateResponse(resp, false))
}

func (f jsonFormatter) FormatUpdate(w io.Writer, resp svc.UpdateResponse) error {
	alreadyCurrent := !resp.SyncPerformed && len(resp.Build.Result.Published) == 0 && len(resp.Build.Result.Skipped) == 0
	return f.encode(w, toJSONUpdateResponse(resp, alreadyCurrent))
}
