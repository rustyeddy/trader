package clock

import "time"

// Real is a Clock backed by the standard library's time package. Its zero
// value is ready to use; it wraps no state.
type Real struct{}

var _ Clock = Real{}

// Now returns time.Now, converted to UTC with any monotonic-clock reading
// stripped — see the package doc comment for why.
func (Real) Now() time.Time {
	return time.Now().UTC().Round(0)
}

// NewTimer starts a real time.Timer and wraps it to satisfy Timer.
func (Real) NewTimer(d time.Duration) Timer {
	return &realTimer{t: time.NewTimer(d)}
}

// realTimer adapts *time.Timer to the Timer interface. time.Timer already
// provides every guarantee Timer documents — a buffered, never-closed
// channel and Stop's transition semantics — so this is a direct pass
// through with no additional logic.
type realTimer struct {
	t *time.Timer
}

func (r *realTimer) C() <-chan time.Time {
	return r.t.C
}

func (r *realTimer) Stop() bool {
	return r.t.Stop()
}
