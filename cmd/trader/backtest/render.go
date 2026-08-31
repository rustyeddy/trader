package backtest

import (
	"fmt"
	"io"

	"github.com/rustyeddy/trader/report"
)

// Format names accepted by --format on both "run" and "show". Unlike
// cmd/trader/execution's or cmd/trader/data's own per-response-shape
// Formatter interface, "run" and "show" both render exactly one type
// (report.BacktestReport), so a plain name-to-Renderer lookup is
// sufficient — no bespoke Formatter interface needed.
const (
	formatTable = "table"
	formatJSON  = "json"
	formatOrg   = "org"
)

func resolveRenderer(format string) (report.Renderer[report.BacktestReport], error) {
	switch format {
	case formatTable, "":
		return report.TextRenderer{}, nil
	case formatJSON:
		return report.JSONRenderer{}, nil
	case formatOrg:
		return report.OrgRenderer{}, nil
	default:
		return nil, fmt.Errorf("invalid --format %q: expected one of %s, %s, %s", format, formatTable, formatJSON, formatOrg)
	}
}

func render(w io.Writer, format string, rep report.BacktestReport) error {
	renderer, err := resolveRenderer(format)
	if err != nil {
		return err
	}
	return renderer.Render(w, rep)
}
