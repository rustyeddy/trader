package analysis

import "fmt"

// Horizon is one forward-return measurement point, expressed as a
// number of bars ahead of the observation bar.
//
// docs/research/mr-01-experiment-definition.org pins its four
// horizons (4h, 12h, 24h, 48h) against H1 bars, where one bar equals
// one hour, so a horizon's Bars count and its intended hour count
// coincide. RunEventStudy itself only ever advances Bars positions
// through whatever bar slice it is given, regardless of the actual
// bar cadence — but EventStudyConfig.validate checks an hour-labeled
// Horizon (as NewH1Horizon produces) against EventStudyConfig.Interval
// whenever Interval has a fixed, calendar-independent bar duration, so
// an "Nh" horizon built assuming H1 bars cannot silently be run
// against a different cadence undetected. NewH1Horizon exists to make
// that assumption explicit and checkable at the call site rather than
// leaving it implicit.
type Horizon struct {
	// Label is a short, stable, human- and machine-readable name for
	// the horizon (for example "4h").
	Label string
	// Bars is the number of bars ahead of the observation bar this
	// horizon measures the forward return at. Bars must be positive.
	Bars int
}

// NewH1Horizon returns the Horizon for hours H1 bars ahead, labeled
// "<hours>h". hours must be positive.
func NewH1Horizon(hours int) (Horizon, error) {
	if hours <= 0 {
		return Horizon{}, ErrInvalidHorizon
	}
	return Horizon{Label: fmt.Sprintf("%dh", hours), Bars: hours}, nil
}

// MR01Horizons returns the four forward-return horizons
// docs/research/mr-01-experiment-definition.org pins: 4h, 12h, 24h,
// and 48h against H1 bars, in that order.
func MR01Horizons() []Horizon {
	// NewH1Horizon cannot fail for these fixed, positive literals.
	h4, _ := NewH1Horizon(4)
	h12, _ := NewH1Horizon(12)
	h24, _ := NewH1Horizon(24)
	h48, _ := NewH1Horizon(48)
	return []Horizon{h4, h12, h24, h48}
}
