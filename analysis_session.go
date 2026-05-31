package trader

import (
	"fmt"
	"time"
)

type hourBucket struct {
	count      int
	totalRange float64
}

// SessionAnalyzer breaks down candle activity and average range by UTC hour.
// Useful for identifying which trading sessions (London, NY, Tokyo) are most
// active and volatile for a given instrument.
type SessionAnalyzer struct {
	unitsPerPip float64
	hours       [24]hourBucket
}

// NewSessionAnalyzer creates a SessionAnalyzer. unitsPerPip is the number of
// Price units that equal one pip for the instrument.
func NewSessionAnalyzer(unitsPerPip float64) *SessionAnalyzer {
	return &SessionAnalyzer{unitsPerPip: unitsPerPip}
}

func (a *SessionAnalyzer) Name() string { return "Session (by UTC hour)" }

func (a *SessionAnalyzer) Update(ct *CandleTime) {
	rng := ct.High - ct.Low
	if rng <= 0 {
		return
	}
	h := time.Unix(int64(ct.Timestamp), 0).UTC().Hour()
	a.hours[h].count++
	a.hours[h].totalRange += float64(rng) / a.unitsPerPip
}

func (a *SessionAnalyzer) Stats() []Stat {
	stats := make([]Stat, 0, 24)
	for h := range 24 {
		b := a.hours[h]
		if b.count == 0 {
			continue
		}
		avg := b.totalRange / float64(b.count)
		stats = append(stats, Stat{
			Name:  fmt.Sprintf("%02d:00 UTC", h),
			Value: fmt.Sprintf("count=%-6d  avg range=%.1f pips", b.count, avg),
		})
	}
	return stats
}
