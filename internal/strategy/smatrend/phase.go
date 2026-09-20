package smatrend

// Phase is smatrend's own explicit position lifecycle state (issue
// #349, the SMA Long Hold playbook), made a first-class, queryable
// concept via Strategy.Phase rather than living only as private state
// inside one ExitRule implementation (PR #348/#349 review).
type Phase uint8

const (
	// PhaseFlat is the zero value: no open position. Also what
	// Strategy.Phase reports for any ExitRule with no Probation/
	// Trending distinction of its own (trailing-stop, sma-cross),
	// even while a position is open under one of those rules.
	PhaseFlat Phase = iota
	// PhaseProbation is a newly entered position that has not yet
	// reached the configured trail-activation gain: protected by a
	// tight stop just below the SMA, and also exited outright if a
	// completed close falls back to or below the SMA. Only ever
	// reported by the "probation-trend" ExitRule.
	PhaseProbation
	// PhaseTrending is a position that has reached trail activation:
	// protected only by a monotonic trailing stop off its own
	// high-water mark since entry; a close below the SMA no longer
	// exits it. Only ever reported by the "probation-trend" ExitRule.
	PhaseTrending
)

// String renders Phase for logging/journaling. Never parsed back —
// this is a display convenience only, matching marketdata.Interval's
// own String doc convention.
func (p Phase) String() string {
	switch p {
	case PhaseFlat:
		return "flat"
	case PhaseProbation:
		return "probation"
	case PhaseTrending:
		return "trending"
	default:
		return "unknown"
	}
}
