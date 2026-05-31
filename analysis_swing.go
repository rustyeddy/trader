package trader

import "fmt"

// SwingAnalyzer measures the high-low range (in pips) of each candle.
type SwingAnalyzer struct {
	unitsPerPip float64
	ranges      []float64
}

// NewSwingAnalyzer creates a SwingAnalyzer.  unitsPerPip is the number of
// Price units that equal one pip for the instrument (e.g. 10 for EURUSD).
func NewSwingAnalyzer(unitsPerPip float64) *SwingAnalyzer {
	return &SwingAnalyzer{unitsPerPip: unitsPerPip}
}

func (a *SwingAnalyzer) Name() string { return "Swing (High-Low Range)" }

func (a *SwingAnalyzer) Update(ct *CandleTime) {
	delta := ct.High - ct.Low
	if delta <= 0 {
		return
	}
	a.ranges = append(a.ranges, float64(delta)/a.unitsPerPip)
}

func (a *SwingAnalyzer) Stats() []Stat {
	if len(a.ranges) == 0 {
		return []Stat{{Name: "count", Value: "0"}}
	}
	sorted := sortedCopy(a.ranges)
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))
	return []Stat{
		{Name: "count", Value: fmt.Sprintf("%d", len(sorted))},
		{Name: "mean", Value: fmt.Sprintf("%.1f pips", mean)},
		{Name: "min", Value: fmt.Sprintf("%.1f pips", sorted[0])},
		{Name: "p25", Value: fmt.Sprintf("%.1f pips", percentile(sorted, 25))},
		{Name: "p50", Value: fmt.Sprintf("%.1f pips", percentile(sorted, 50))},
		{Name: "p75", Value: fmt.Sprintf("%.1f pips", percentile(sorted, 75))},
		{Name: "p90", Value: fmt.Sprintf("%.1f pips", percentile(sorted, 90))},
		{Name: "max", Value: fmt.Sprintf("%.1f pips", sorted[len(sorted)-1])},
	}
}
