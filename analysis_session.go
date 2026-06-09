package trader

import (
	"fmt"
	"time"
)

type hourBucket struct {
	count      int
	totalRange int64 // sum of Price deltas
}

// SessionAnalyzer breaks down candle activity and average range by UTC hour.
// Ranges are stored as Price (scaled int) and converted to pips only at output.
type SessionAnalyzer struct {
	inst  *Instrument
	hours [24]hourBucket
}

// NewSessionAnalyzer creates a SessionAnalyzer for the given instrument.
func NewSessionAnalyzer(inst *Instrument) *SessionAnalyzer {
	return &SessionAnalyzer{inst: inst}
}

func (a *SessionAnalyzer) Name() string { return "Session (by UTC hour)" }

func (a *SessionAnalyzer) Update(ct *CandleTime) {
	if !ct.Candle.Validate() {
		return
	}
	rng := ct.High - ct.Low
	h := time.Unix(int64(ct.Timestamp), 0).UTC().Hour()
	a.hours[h].count++
	a.hours[h].totalRange += int64(rng)
}

func (a *SessionAnalyzer) Stats() []Stat {
	if a.inst == nil {
		return missingInstrumentStats()
	}
	uPip := float64(a.inst.PriceUnitsPerPip())
	stats := make([]Stat, 0, 24)
	for h := range 24 {
		b := a.hours[h]
		if b.count == 0 {
			continue
		}
		avg := float64(b.totalRange) / float64(b.count) / uPip
		stats = append(stats, Stat{
			Name:  fmt.Sprintf("%02d:00 UTC", h),
			Value: fmt.Sprintf("count=%-6d  avg range=%.1f pips", b.count, avg),
			Pips:  avg,
		})
	}
	return stats
}
