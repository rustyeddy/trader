package chart

import (
	"time"

	"gonum.org/v1/plot"
)

// unixSeconds converts t to the float64 x-value every plotted point
// and tick in this package uses for its time axis — plain Unix
// seconds, UTC. A direct numeric conversion, not a round-trip through
// any text representation, so it introduces no precision loss and no
// non-determinism of its own.
func unixSeconds(t time.Time) float64 {
	return float64(t.UTC().Unix())
}

// dateTicker implements plot.Ticker, labeling a chart's own x-axis
// with calendar dates (YYYY-MM-DD) instead of gonum.org/v1/plot's own
// default raw-Unix-seconds tick labels. Deterministic: Ticks is a
// pure function of min/max, calling no clock and reading no external
// state.
type dateTicker struct{}

// Ticks implements plot.Ticker. It picks a small, fixed number of
// evenly spaced tick positions across [min, max] — this package's own
// charts are research-inspection tools read at a fixed, moderate
// zoom level, not an interactive/zoomable axis that needs
// density-adaptive tick spacing.
func (dateTicker) Ticks(min, max float64) []plot.Tick {
	const wantTicks = 8
	if max <= min {
		return []plot.Tick{{Value: min, Label: formatUnix(min)}}
	}
	step := (max - min) / float64(wantTicks-1)
	ticks := make([]plot.Tick, 0, wantTicks)
	for i := 0; i < wantTicks; i++ {
		v := min + step*float64(i)
		ticks = append(ticks, plot.Tick{Value: v, Label: formatUnix(v)})
	}
	return ticks
}

func formatUnix(v float64) string {
	return time.Unix(int64(v), 0).UTC().Format("2006-01-02")
}
