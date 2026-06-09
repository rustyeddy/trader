package trader

import "fmt"

const trendRatioScale = 1_000_000

// TrendAnalyzer measures the body/range ratio as a proxy for trending vs
// consolidating bars.  ratio = |Close−Open| / (High−Low).
//
// Thresholds: >600 → trending; <300 → consolidating.
type TrendAnalyzer struct {
	total         int
	trending      int
	consolidating int
	ratioSum      int64 // sum of ratio values scaled by trendRatioScale
}

// NewTrendAnalyzer creates a TrendAnalyzer.
func NewTrendAnalyzer() *TrendAnalyzer {
	return &TrendAnalyzer{}
}

func (a *TrendAnalyzer) Name() string { return "Trend vs Consolidation" }

func (a *TrendAnalyzer) Update(ct *CandleTime) {
	if !validOHLC(ct.Candle) {
		return
	}
	rng := int64(ct.High - ct.Low)
	body := int64(ct.Close - ct.Open)
	if body < 0 {
		body = -body
	}
	ratioScaled := body * trendRatioScale / rng
	a.total++
	a.ratioSum += ratioScaled
	switch {
	case body*10 > rng*6:
		a.trending++
	case body*10 < rng*3:
		a.consolidating++
	}
}

func (a *TrendAnalyzer) Stats() []Stat {
	if a.total == 0 {
		return []Stat{{Name: "count", Value: "0"}}
	}
	mixed := a.total - a.trending - a.consolidating
	meanRatio := float64(a.ratioSum) / float64(a.total) / trendRatioScale
	pct := func(n int) string {
		return fmt.Sprintf("%.1f%%  (%d)", 100*float64(n)/float64(a.total), n)
	}
	return []Stat{
		{Name: "count", Value: fmt.Sprintf("%d", a.total)},
		{Name: "mean body/range", Value: fmt.Sprintf("%.3f", meanRatio)},
		{Name: "trending  (>0.6)", Value: pct(a.trending)},
		{Name: "mixed  (0.3–0.6)", Value: pct(mixed)},
		{Name: "consolidating  (<0.3)", Value: pct(a.consolidating)},
	}
}
