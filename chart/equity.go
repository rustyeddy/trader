package chart

import (
	"fmt"
	"image/color"
	"io"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

// DefaultEquitySize is RenderEquity's own default output size.
var DefaultEquitySize = Size{WidthInches: 12, HeightInches: 5}

// RenderEquity renders in's own equity curve (issue #355), optionally
// compared against a buy-and-hold series over the identical span, to
// w in format, at DefaultEquitySize. Returns ErrEmptyEquity if
// in.Equity is empty.
func RenderEquity(w io.Writer, in EquityInput, format Format) error {
	if len(in.Equity) == 0 {
		return ErrEmptyEquity
	}

	p := plot.New()
	p.Title.Text = in.Title
	p.X.Tick.Marker = dateTicker{}
	p.Y.Label.Text = "Equity"

	equityLine, err := newLevelLine(in.Equity, closeLineColor)
	if err != nil {
		return fmt.Errorf("chart: building equity line: %w", err)
	}
	equityLine.Width = vg.Points(1.5)
	p.Add(equityLine)
	p.Legend.Add("Strategy", equityLine)

	if len(in.BuyAndHold) > 0 {
		bhLine, err := newLevelLine(in.BuyAndHold, buyHoldColor)
		if err != nil {
			return fmt.Errorf("chart: building buy-and-hold line: %w", err)
		}
		bhLine.Width = vg.Points(1)
		bhLine.Dashes = []vg.Length{vg.Points(4), vg.Points(3)}
		p.Add(bhLine)
		p.Legend.Add("Buy & Hold", bhLine)
	}

	return writePlot(w, p, DefaultEquitySize, format)
}

func newLevelLine(points []EquityPoint, col color.Color) (*plotter.Line, error) {
	pts := make(plotter.XYs, len(points))
	for i, ep := range points {
		pts[i].X = unixSeconds(ep.Time)
		pts[i].Y = ep.Value
	}
	line, err := plotter.NewLine(pts)
	if err != nil {
		return nil, err
	}
	line.Color = col
	return line, nil
}
