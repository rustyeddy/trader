package trader

import "fmt"

// TrendAnalyzer measures the body/range ratio as a proxy for trending vs
// consolidating bars.  ratio = |Close−Open| / (High−Low).
//
// Integer fixed-point: ratio is stored as ratio×1000 (0–1000).
// Thresholds: >600 → trending; <300 → consolidating.
type TrendAnalyzer struct {
	total         int
	trending      int
	consolidating int
	ratioSum      int64 // sum of ratio×1000 values
}

// NewTrendAnalyzer creates a TrendAnalyzer.
func NewTrendAnalyzer() *TrendAnalyzer {
	return &TrendAnalyzer{}
}

func (a *TrendAnalyzer) Name() string { return "Trend vs Consolidation" }

func (a *TrendAnalyzer) Update(ct *CandleTime) {
	rng := int64(ct.High - ct.Low)
	if rng <= 0 {
		return
	}
	body := int64(ct.Close - ct.Open)
	if body < 0 {
		body = -body
	}
	ratio1000 := body * 1000 / rng
	a.total++
	a.ratioSum += ratio1000
	switch {
	case ratio1000 > 600:
		a.trending++
	case ratio1000 < 300:
		a.consolidating++
	}
}

func (a *TrendAnalyzer) Stats() []Stat {
	if a.total == 0 {
		return []Stat{{Name: "count", Value: "0"}}
	}
	mixed := a.total - a.trending - a.consolidating
	meanRatio := float64(a.ratioSum) / float64(int64(a.total)*1000)
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
