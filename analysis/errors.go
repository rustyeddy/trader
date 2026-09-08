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

	// ErrMissingInstrument is returned when an EventStudyConfig's
	// Instrument is the zero instrument.ID.
	ErrMissingInstrument = errors.New("analysis: instrument is required")

	// ErrMissingInterval is returned when an EventStudyConfig's
	// Interval is not Valid().
	ErrMissingInterval = errors.New("analysis: interval is required")

	// ErrHorizonIntervalMismatch is returned when an hour-labeled
	// Horizon (Label matching "<N>h") does not actually total that
	// many hours at the configured Interval's bar duration — see
	// validateHorizonLabel.
	ErrHorizonIntervalMismatch = errors.New("analysis: horizon label does not match interval")

	// ErrUnknownBucket is returned by Bucket.UnmarshalText for any
	// text other than one of the five labels Bucket.String produces.
	ErrUnknownBucket = errors.New("analysis: unknown bucket label")

	// ErrUnknownRSIBucket is returned by RSIBucket.UnmarshalText for
	// any text other than one of the six labels RSIBucket.String
	// produces.
	ErrUnknownRSIBucket = errors.New("analysis: unknown RSI bucket label")

	// ErrUnknownRegime is returned by Regime.UnmarshalText for any text
	// other than one of Regime.String's two labels.
	ErrUnknownRegime = errors.New("analysis: unknown regime label")

	// ErrInvalidRSIPeriod is returned when an RSIRegimeEventStudyConfig's
	// RSIPeriod is non-positive.
	ErrInvalidRSIPeriod = errors.New("analysis: RSI period must be positive")

	// ErrInvalidEMAPeriod is returned when an RSIRegimeEventStudyConfig's
	// EMAPeriod is non-positive.
	ErrInvalidEMAPeriod = errors.New("analysis: EMA period must be positive")

	// ErrInvalidMinObservations is returned when an
	// RSIRegimeEventStudyConfig's MinObservations is non-positive.
	ErrInvalidMinObservations = errors.New("analysis: minimum observation count must be positive")

	// ErrInvalidBootstrapResamples is returned when a BootstrapConfig's
	// Resamples is non-positive.
	ErrInvalidBootstrapResamples = errors.New("analysis: bootstrap resample count must be positive")
)
