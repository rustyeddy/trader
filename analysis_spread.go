package trader

import "fmt"

// SpreadAnalyzer measures the AvgSpread value of each candle.
// AvgSpread values are stored as Price (scaled int) and converted to pips only at output.
// Candles with zero AvgSpread are skipped (tick data may not carry spread).
type SpreadAnalyzer struct {
	inst    *Instrument
	spreads priceDistribution
}

// NewSpreadAnalyzer creates a SpreadAnalyzer for the given instrument.
func NewSpreadAnalyzer(inst *Instrument) *SpreadAnalyzer {
	return &SpreadAnalyzer{inst: inst}
}

func (a *SpreadAnalyzer) Name() string { return "Average Spread" }

func (a *SpreadAnalyzer) Update(ct *CandleTime) {
	if !ct.Candle.Validate() {
		return
	}
	if ct.AvgSpread <= 0 {
		return
	}
	a.spreads.Add(ct.AvgSpread)
}

func (a *SpreadAnalyzer) Stats() []Stat {
	if a.inst == nil {
		return missingInstrumentStats()
	}
	if a.spreads.Len() == 0 {
		return []Stat{{Name: "count (with avg spread)", Value: "0"}}
	}
	uPip := unitsPerPip(a.inst)
	pip := func(name string, v float64) Stat {
		return Stat{Name: name, Value: fmt.Sprintf("%.2f pips", v), Pips: v}
	}
	return []Stat{
		{Name: "count (with avg spread)", Value: fmt.Sprintf("%d", a.spreads.Len())},
		pip("mean", a.spreads.MeanPips(uPip)),
		pip("p90", a.spreads.PercentilePips(90, uPip)),
		pip("max", a.spreads.MaxPips(uPip)),
	}
}
