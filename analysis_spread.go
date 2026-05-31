package trader

import "fmt"

// SpreadAnalyzer measures the average spread (in pips) recorded on each candle.
// Candles with zero AvgSpread are skipped (tick data may not carry spread).
type SpreadAnalyzer struct {
	unitsPerPip float64
	spreads     []float64
}

// NewSpreadAnalyzer creates a SpreadAnalyzer. unitsPerPip is the number of
// Price units that equal one pip for the instrument.
func NewSpreadAnalyzer(unitsPerPip float64) *SpreadAnalyzer {
	return &SpreadAnalyzer{unitsPerPip: unitsPerPip}
}

func (a *SpreadAnalyzer) Name() string { return "Spread" }

func (a *SpreadAnalyzer) Update(ct *CandleTime) {
	if ct.AvgSpread <= 0 {
		return
	}
	a.spreads = append(a.spreads, float64(ct.AvgSpread)/a.unitsPerPip)
}

func (a *SpreadAnalyzer) Stats() []Stat {
	if len(a.spreads) == 0 {
		return []Stat{{Name: "count (with spread)", Value: "0"}}
	}
	sorted := sortedCopy(a.spreads)
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))
	return []Stat{
		{Name: "count (with spread)", Value: fmt.Sprintf("%d", len(sorted))},
		{Name: "mean", Value: fmt.Sprintf("%.2f pips", mean)},
		{Name: "p90", Value: fmt.Sprintf("%.2f pips", percentile(sorted, 90))},
		{Name: "max", Value: fmt.Sprintf("%.2f pips", sorted[len(sorted)-1])},
	}
}
