package trader

import "fmt"

// SessionFilter is a regime filter that restricts entries to a specified
// UTC hour window. Bars outside the window return Trending() = false so
// the strategy skips new opens. The filter has no warmup requirement.
//
// Default window: 07:00–17:00 UTC (London open through NY afternoon).
// Registered in the factory as "session".
type SessionFilter struct {
	start int // inclusive UTC hour (0-23)
	end   int // exclusive UTC hour (1-24)

	// current UTC hour, updated on every Tick
	hour int
	ready bool
}

func NewSessionFilter(start, end int) *SessionFilter {
	return &SessionFilter{start: start, end: end}
}

func (f *SessionFilter) Name() string {
	return fmt.Sprintf("Session(%02d:00-%02d:00UTC)", f.start, f.end)
}

func (f *SessionFilter) Ready() bool { return f.ready }

func (f *SessionFilter) Tick(ct CandleTime) {
	f.hour = int((int64(ct.Timestamp) % 86400) / 3600)
	f.ready = true
}

func (f *SessionFilter) Trending() bool {
	if !f.ready {
		return true // allow during warmup (shouldn't happen, but be safe)
	}
	return f.hour >= f.start && f.hour < f.end
}

func (f *SessionFilter) AllowSide(_ Side) bool { return true }
