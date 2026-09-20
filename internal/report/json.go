package report

import (
	"encoding/json"
	"io"
)

// JSONRenderer renders a BacktestReport as indented JSON, using
// BacktestReport's own json tags as the wire contract — never
// json.Marshal(backtest.Result) or backtest.Metrics directly (issue
// #220 review, point 3), so a backtest-internal field rename cannot
// silently change this format. Every value in BacktestReport already
// marshals deterministically: num's own types implement json.
// Marshaler with a fixed decimal representation, nil metric pointers
// marshal as null, and every timestamp was normalized to UTC by
// NewBacktestReport.
type JSONRenderer struct{}

// Render implements Renderer[BacktestReport].
func (JSONRenderer) Render(w io.Writer, report BacktestReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
