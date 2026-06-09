package trader

import "fmt"

// SwingAnalyzer measures the high-low range of each candle.
// Ranges are stored as Price (scaled int) and converted to pips only at output.
type SwingAnalyzer struct {
	inst   *Instrument
	ranges priceDistribution
}

// NewSwingAnalyzer creates a SwingAnalyzer for the given instrument.
func NewSwingAnalyzer(inst *Instrument) *SwingAnalyzer {
	return &SwingAnalyzer{inst: inst}
}

func (a *SwingAnalyzer) Name() string { return "Swing Range Distribution" }

func (a *SwingAnalyzer) Update(ct *CandleTime) {
	if !ct.Candle.Validate() {
		return
	}
	delta := ct.High - ct.Low
	a.ranges.Add(delta)
}

func (a *SwingAnalyzer) Stats() []Stat {
	if a.inst == nil {
		return missingInstrumentStats()
	}
	if a.ranges.Len() == 0 {
		return []Stat{{Name: "count", Value: "0"}}
	}
	uPip := float64(a.inst.PriceUnitsPerPip())
	sorted := a.ranges.sortedPrices()
	return []Stat{
		{Name: "count", Value: fmt.Sprintf("%d", a.ranges.Len())},
		pipStat("mean", a.ranges.MeanPips(uPip), 1),
		pipStat("p0", a.ranges.percentilePips(0, uPip, sorted), 1),
		pipStat("min", a.ranges.MinPips(uPip), 1),
		pipStat("p25", a.ranges.percentilePips(25, uPip, sorted), 1),
		pipStat("p50", a.ranges.percentilePips(50, uPip, sorted), 1),
		pipStat("p75", a.ranges.percentilePips(75, uPip, sorted), 1),
		pipStat("p90", a.ranges.percentilePips(90, uPip, sorted), 1),
		pipStat("max", a.ranges.MaxPips(uPip), 1),
	}
}
