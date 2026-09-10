package chart

import "io"

// DefaultEpisodeSize is RenderEpisode's own default output size.
var DefaultEpisodeSize = Size{WidthInches: 10, HeightInches: 5}

// RenderEpisode renders in's own bounded exit/re-entry episode chart
// (issue #355) — the windowed Close-price series, optional SMA/
// probation-stop/trailing-stop overlays, and this episode's own
// markers (typically entry, exit, post-exit trough, and re-entry) —
// to w in format, at DefaultEpisodeSize. Returns ErrEmptyBars if
// in.Bars is empty.
func RenderEpisode(w io.Writer, in EpisodeInput, format Format) error {
	extra := []namedSeries{
		{name: "SMA", points: in.SMA, color: smaLineColor},
		{name: "Probation Stop", points: in.ProbationStop, color: probationColor, dashed: true},
		{name: "Trailing Stop", points: in.TrailingStop, color: trailingColor, dashed: true},
	}
	p, err := newPricePlot(in.Title, in.Bars, extra, in.Markers)
	if err != nil {
		return err
	}
	return writePlot(w, p, DefaultEpisodeSize, format)
}
