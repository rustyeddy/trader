package analysis

import "errors"

var (
	// ErrInvalidHorizon is returned when a Horizon is constructed with
	// a non-positive bar count.
	ErrInvalidHorizon = errors.New("analysis: horizon bars must be positive")

	// ErrInvalidZScorePeriod is returned when an EventStudyConfig's
	// ZScorePeriod is non-positive.
	ErrInvalidZScorePeriod = errors.New("analysis: z-score period must be positive")

	// ErrNoHorizons is returned when an EventStudyConfig has no
	// configured Horizons.
	ErrNoHorizons = errors.New("analysis: at least one horizon is required")
)
