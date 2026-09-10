package chart

import (
	"fmt"
	"image/color"
	"io"

	"github.com/rustyeddy/trader/marketdata"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
)

// seriesColor and markerStyle are fixed, never randomized or
// input-dependent — part of what keeps this package's output
// deterministic for identical input (doc.go's own determinism note),
// and keeps a marker's meaning consistent across every chart this
// package ever renders.
var (
	closeLineColor = color.RGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff} // near-black
	smaLineColor   = color.RGBA{R: 0x1f, G: 0x77, B: 0xb4, A: 0xff} // blue
	probationColor = color.RGBA{R: 0x94, G: 0x67, B: 0xbd, A: 0xff} // purple
	trailingColor  = color.RGBA{R: 0xe3, G: 0x8f, B: 0x22, A: 0xff} // amber
	buyHoldColor   = color.RGBA{R: 0x7f, G: 0x7f, B: 0x7f, A: 0xff} // gray
)

func markerStyle(kind MarkerKind) draw.GlyphStyle {
	base := draw.GlyphStyle{Radius: vg.Points(4)}
	switch kind {
	case MarkerEntry:
		base.Color = color.RGBA{G: 0x99, A: 0xff}
		base.Shape = draw.TriangleGlyph{}
	case MarkerExit:
		base.Color = color.RGBA{R: 0xcc, A: 0xff}
		base.Shape = draw.CrossGlyph{}
	case MarkerReentry:
		base.Color = color.RGBA{B: 0xcc, A: 0xff}
		base.Shape = draw.CircleGlyph{}
	case MarkerTrough:
		base.Color = color.RGBA{R: 0xe3, G: 0x8f, B: 0x22, A: 0xff}
		base.Shape = draw.PlusGlyph{}
	default:
		base.Color = color.Black
		base.Shape = draw.RingGlyph{}
	}
	return base
}

// newPricePlot builds a *plot.Plot with a date-formatted x-axis, a
// Close-price line from bars, and every non-empty extra named series
// added as its own colored line — shared by newOverviewPlot and
// newEpisodePlot, the two chart types built from bars plus overlay
// series plus markers. Returns ErrEmptyBars if bars is empty: a chart
// with no price series is never a valid, silently degraded chart.
func newPricePlot(title string, bars []marketdata.Bar, extra []namedSeries, markers []Marker) (*plot.Plot, error) {
	if len(bars) == 0 {
		return nil, ErrEmptyBars
	}

	p := plot.New()
	p.Title.Text = title
	p.X.Tick.Marker = dateTicker{}
	p.Y.Label.Text = "Price"

	closePts := make(plotter.XYs, len(bars))
	for i, b := range bars {
		closePts[i].X = unixSeconds(b.Time)
		closePts[i].Y = b.Close.Float64()
	}
	closeLine, err := plotter.NewLine(closePts)
	if err != nil {
		return nil, fmt.Errorf("chart: building close-price line: %w", err)
	}
	closeLine.Color = closeLineColor
	closeLine.Width = vg.Points(1.2)
	p.Add(closeLine)
	p.Legend.Add("Close", closeLine)

	for _, s := range extra {
		if len(s.points) == 0 {
			continue
		}
		pts := make(plotter.XYs, len(s.points))
		for i, lp := range s.points {
			pts[i].X = unixSeconds(lp.Time)
			pts[i].Y = lp.Price.Float64()
		}
		line, err := plotter.NewLine(pts)
		if err != nil {
			return nil, fmt.Errorf("chart: building %s line: %w", s.name, err)
		}
		line.Color = s.color
		line.Width = vg.Points(1)
		if s.dashed {
			line.Dashes = []vg.Length{vg.Points(4), vg.Points(3)}
		}
		p.Add(line)
		p.Legend.Add(s.name, line)
	}

	if err := addMarkers(p, markers); err != nil {
		return nil, err
	}

	return p, nil
}

// namedSeries pairs a LevelPoint series with the styling newPricePlot
// draws it with — an internal-only pairing so newPricePlot itself
// never has to know which optional overlay (SMA, probation stop,
// trailing stop) it is drawing.
type namedSeries struct {
	name   string
	points []LevelPoint
	color  color.Color
	dashed bool
}

// addMarkers adds one plotter.Scatter per distinct MarkerKind present
// in markers (grouped, not one Scatter per point) so each kind gets
// exactly one legend entry regardless of how many markers of that
// kind exist.
func addMarkers(p *plot.Plot, markers []Marker) error {
	byKind := make(map[MarkerKind]plotter.XYs)
	order := make([]MarkerKind, 0, 4)
	var labelPts plotter.XYLabels
	for _, m := range markers {
		if _, ok := byKind[m.Kind]; !ok {
			order = append(order, m.Kind)
		}
		byKind[m.Kind] = append(byKind[m.Kind], plotter.XY{X: unixSeconds(m.Time), Y: m.Price.Float64()})

		if m.Label != "" {
			labelPts.XYs = append(labelPts.XYs, plotter.XY{X: unixSeconds(m.Time), Y: m.Price.Float64()})
			labelPts.Labels = append(labelPts.Labels, m.Label)
		}
	}
	for _, kind := range order {
		sc, err := plotter.NewScatter(byKind[kind])
		if err != nil {
			return fmt.Errorf("chart: building %s markers: %w", kind, err)
		}
		sc.GlyphStyle = markerStyle(kind)
		p.Add(sc)
		p.Legend.Add(string(kind), sc)
	}

	// Marker.Label is rendered as small text offset above each labeled
	// point (issue #355 review): the field is part of the documented
	// public contract specifically so a research chart can distinguish
	// same-Kind markers from each other (for example two MarkerExit
	// points, one "probation stop" and one "gap-through") — a
	// documented field that addMarkers silently ignored would be a
	// contract the API promises but never keeps.
	if len(labelPts.XYs) > 0 {
		labels, err := plotter.NewLabels(labelPts)
		if err != nil {
			return fmt.Errorf("chart: building marker labels: %w", err)
		}
		labels.Offset = vg.Point{X: 0, Y: vg.Points(8)}
		p.Add(labels)
	}
	return nil
}

// writePlot renders p at size in format to w — the one place every
// renderer in this package actually produces bytes, so Format's own
// vendor-format-string translation and every renderer's error
// wrapping stay in one place.
func writePlot(w io.Writer, p *plot.Plot, size Size, format Format) error {
	vgFmt, err := format.vgFormat()
	if err != nil {
		return err
	}
	wt, err := p.WriterTo(vg.Length(size.WidthInches)*vg.Inch, vg.Length(size.HeightInches)*vg.Inch, vgFmt)
	if err != nil {
		return fmt.Errorf("chart: preparing %s output: %w", format, err)
	}
	if _, err := wt.WriteTo(w); err != nil {
		return fmt.Errorf("chart: writing %s output: %w", format, err)
	}
	return nil
}
