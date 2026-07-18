package datamanager

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
)

const (
	trendRatioScale                 int64 = 1_000_000
	trendThresholdNumerator         int64 = 6
	consolidationThresholdNumerator int64 = 3
	trendThresholdDenominator       int64 = 10
)

// TrendAnalyzer measures the body/range ratio as a proxy for trending vs
// consolidating bars.  ratio = |Close−Open| / (High−Low).
//
// Thresholds: >0.6 → trending; <0.3 → consolidating.
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

func (a *TrendAnalyzer) Name() string { return "Trend Distribution" }

func (a *TrendAnalyzer) Update(ct *market.Candle) {
	if !ct.Validate() {
		return
	}
	rng := int64(ct.High) - int64(ct.Low)
	if rng == 0 {
		return // flat candle — no range to compute ratio against
	}
	body := int64(ct.Close) - int64(ct.Open)
	if body < 0 {
		body = -body
	}
	ratioScaled := (body*trendRatioScale + rng/2) / rng
	a.total++
	a.ratioSum += ratioScaled
	switch {
	case body*trendThresholdDenominator > rng*trendThresholdNumerator:
		a.trending++
	case body*trendThresholdDenominator < rng*consolidationThresholdNumerator:
		a.consolidating++
	}
}

func (a *TrendAnalyzer) Stats() []Stat {
	if a.total == 0 {
		return []Stat{{Name: "count", Value: "0"}}
	}
	mixed := a.total - a.trending - a.consolidating
	meanRatio := float64(a.ratioSum) / float64(a.total) / float64(trendRatioScale)
	pct := func(n int) string {
		return fmt.Sprintf("%.1f%%  (%d)", 100*float64(n)/float64(a.total), n)
	}
	return []Stat{
		{Name: "count", Value: fmt.Sprintf("%d", a.total)},
		{Name: "mean body/range", Value: fmt.Sprintf("%.3f", meanRatio)},
		{Name: "trending (>0.6)", Value: pct(a.trending)},
		{Name: "mixed (0.3–0.6)", Value: pct(mixed)},
		{Name: "consolidating (<0.3)", Value: pct(a.consolidating)},
	}
}
