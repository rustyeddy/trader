package trader

import "fmt"

// TrendAnalyzer measures the body/range ratio as a proxy for trending vs
// consolidating bars.  ratio = |Close−Open| / (High−Low).
//
// Thresholds: ratio > 0.6 → trending; ratio < 0.3 → consolidating.
type TrendAnalyzer struct {
	total         int
	trending      int
	consolidating int
	ratioSum      float64
}

// NewTrendAnalyzer creates a TrendAnalyzer.
func NewTrendAnalyzer() *TrendAnalyzer {
	return &TrendAnalyzer{}
}

func (a *TrendAnalyzer) Name() string { return "Trend vs Consolidation" }

func (a *TrendAnalyzer) Update(ct *CandleTime) {
	rng := ct.High - ct.Low
	if rng <= 0 {
		return
	}
	body := ct.Close - ct.Open
	if body < 0 {
		body = -body
	}
	ratio := float64(body) / float64(rng)
	a.total++
	a.ratioSum += ratio
	switch {
	case ratio > 0.6:
		a.trending++
	case ratio < 0.3:
		a.consolidating++
	}
}

func (a *TrendAnalyzer) Stats() []Stat {
	if a.total == 0 {
		return []Stat{{Name: "count", Value: "0"}}
	}
	mixed := a.total - a.trending - a.consolidating
	pct := func(n int) string {
		return fmt.Sprintf("%.1f%%  (%d)", 100*float64(n)/float64(a.total), n)
	}
	return []Stat{
		{Name: "count", Value: fmt.Sprintf("%d", a.total)},
		{Name: "mean body/range", Value: fmt.Sprintf("%.3f", a.ratioSum/float64(a.total))},
		{Name: "trending  (>0.6)", Value: pct(a.trending)},
		{Name: "mixed  (0.3–0.6)", Value: pct(mixed)},
		{Name: "consolidating  (<0.3)", Value: pct(a.consolidating)},
	}
}
