package trader

import "fmt"

// SpreadAnalyzer measures the average spread of each candle.
// Spreads are stored as Price (scaled int) and converted to pips only at output.
// Candles with zero AvgSpread are skipped (tick data may not carry spread).
type SpreadAnalyzer struct {
	inst    *Instrument
	spreads []Price
}

// NewSpreadAnalyzer creates a SpreadAnalyzer for the given instrument.
func NewSpreadAnalyzer(inst *Instrument) *SpreadAnalyzer {
	return &SpreadAnalyzer{inst: inst}
}

func (a *SpreadAnalyzer) Name() string { return "Spread" }

func (a *SpreadAnalyzer) Update(ct *CandleTime) {
	if ct.AvgSpread <= 0 {
		return
	}
	a.spreads = append(a.spreads, ct.AvgSpread)
}

func (a *SpreadAnalyzer) Stats() []Stat {
	if len(a.spreads) == 0 {
		return []Stat{{Name: "count (with spread)", Value: "0"}}
	}
	uPip := unitsPerPip(a.inst)
	pips := pricesToPips(a.spreads, uPip)
	sorted := sortedCopy(pips)
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))
	p90 := percentile(sorted, 90)
	max := sorted[len(sorted)-1]
	pip := func(name string, v float64) Stat {
		return Stat{Name: name, Value: fmt.Sprintf("%.2f pips", v), Pips: v}
	}
	return []Stat{
		{Name: "count (with spread)", Value: fmt.Sprintf("%d", len(sorted))},
		pip("mean", mean),
		pip("p90", p90),
		pip("max", max),
	}
}
