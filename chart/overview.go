package chart

import "io"

// DefaultOverviewSize is RenderOverview's own default output size.
var DefaultOverviewSize = Size{WidthInches: 12, HeightInches: 6}

// RenderOverview renders in's full-run overview chart (issue #355) —
// the complete Close-price series, an optional SMA overlay, and every
// entry/exit/re-entry marker across the whole run — to w in format,
// at DefaultOverviewSize. Returns ErrEmptyBars if in.Bars is empty.
func RenderOverview(w io.Writer, in OverviewInput, format Format) error {
	extra := []namedSeries{{name: "SMA", points: in.SMA, color: smaLineColor}}
	p, err := newPricePlot(in.Title, in.Bars, extra, in.Markers)
	if err != nil {
		return err
	}
	return writePlot(w, p, DefaultOverviewSize, format)
}
