package strategy

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// SessionFilter is a regime filter that restricts entries to a specified
// UTC hour window. Bars outside the window return Trending() = false so
// the strategy skips new opens. Session windows must stay within a single UTC
// day; overnight windows like 22:00-06:00 are not supported. Trending()
// returns true before Ready() as a defensive contract, although the main
// callers already gate on Ready() before consulting the regime state.
//
// Default window: 07:00–17:00 UTC (London open through NY afternoon).
// Registered in the factory as "session".
type SessionFilter struct {
	start int // inclusive UTC hour (0-23)
	end   int // exclusive UTC hour (1-24)

	// current UTC hour, updated on every Tick
	utcHour int
	ready   bool
}

func NewSessionFilter(start, end int) (*SessionFilter, error) {
	if err := validateSessionWindow(start, end); err != nil {
		return nil, err
	}
	return &SessionFilter{start: start, end: end}, nil
}

func (f *SessionFilter) Name() string {
	return fmt.Sprintf("Session(%02d:00-%02d:00UTC)", f.start, f.end)
}

func (f *SessionFilter) Ready() bool { return f.ready }

func (f *SessionFilter) Tick(ct market.CandleTime) {
	f.utcHour = int((int64(ct.Timestamp) % 86400) / 3600)
	f.ready = true
}

func (f *SessionFilter) Trending() bool {
	if !f.ready {
		return true // allow during warmup (shouldn't happen, but be safe)
	}
	return f.utcHour >= f.start && f.utcHour < f.end
}

func (f *SessionFilter) AllowSide(_ types.Side) bool { return true }
