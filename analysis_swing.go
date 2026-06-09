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

func (a *SwingAnalyzer) Name() string { return "Swing (High-Low Range)" }

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
	uPip := unitsPerPip(a.inst)
	pip := func(name string, v float64) Stat {
		return Stat{Name: name, Value: fmt.Sprintf("%.1f pips", v), Pips: v}
	}
	return []Stat{
		{Name: "count", Value: fmt.Sprintf("%d", a.ranges.Len())},
		pip("mean", a.ranges.MeanPips(uPip)),
		pip("min", a.ranges.MinPips(uPip)),
		pip("p25", a.ranges.PercentilePips(25, uPip)),
		pip("p50", a.ranges.PercentilePips(50, uPip)),
		pip("p75", a.ranges.PercentilePips(75, uPip)),
		pip("p90", a.ranges.PercentilePips(90, uPip)),
		pip("max", a.ranges.MaxPips(uPip)),
	}
}
