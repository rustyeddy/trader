package trader

import "fmt"

// SwingAnalyzer measures the high-low range of each candle.
// Ranges are stored as Price (scaled int) and converted to pips only at output.
type SwingAnalyzer struct {
	inst   *Instrument
	ranges []Price
}

// NewSwingAnalyzer creates a SwingAnalyzer for the given instrument.
func NewSwingAnalyzer(inst *Instrument) *SwingAnalyzer {
	return &SwingAnalyzer{inst: inst}
}

func (a *SwingAnalyzer) Name() string { return "Swing (High-Low Range)" }

func (a *SwingAnalyzer) Update(ct *CandleTime) {
	delta := ct.High - ct.Low
	if delta <= 0 {
		return
	}
	a.ranges = append(a.ranges, delta)
}

func (a *SwingAnalyzer) Stats() []Stat {
	if len(a.ranges) == 0 {
		return []Stat{{Name: "count", Value: "0"}}
	}
	uPip := unitsPerPip(a.inst)
	pips := pricesToPips(a.ranges, uPip)
	sorted := sortedCopy(pips)
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))
	p25 := percentile(sorted, 25)
	p50 := percentile(sorted, 50)
	p75 := percentile(sorted, 75)
	p90 := percentile(sorted, 90)
	pip := func(name string, v float64) Stat {
		return Stat{Name: name, Value: fmt.Sprintf("%.1f pips", v), Pips: v}
	}
	return []Stat{
		{Name: "count", Value: fmt.Sprintf("%d", len(sorted))},
		pip("mean", mean),
		pip("min", sorted[0]),
		pip("p25", p25),
		pip("p50", p50),
		pip("p75", p75),
		pip("p90", p90),
		pip("max", sorted[len(sorted)-1]),
	}
}
