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

func (a *SpreadAnalyzer) Name() string { return "Avg Spread Distribution" }

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
		return []Stat{{Name: "count", Value: "0"}}
	}
	uPip := float64(a.inst.PriceUnitsPerPip())
	sorted := a.spreads.sortedPrices()
	return []Stat{
		{Name: "count", Value: fmt.Sprintf("%d", a.spreads.Len())},
		pipStat("mean", a.spreads.MeanPips(uPip), 2),
		pipStat("p0", a.spreads.percentilePips(0, uPip, sorted), 2),
		pipStat("p25", a.spreads.percentilePips(25, uPip, sorted), 2),
		pipStat("p50", a.spreads.percentilePips(50, uPip, sorted), 2),
		pipStat("p75", a.spreads.percentilePips(75, uPip, sorted), 2),
		pipStat("tail (p90)", a.spreads.percentilePips(90, uPip, sorted), 2),
		pipStat("max", a.spreads.MaxPips(uPip), 2),
	}
}
