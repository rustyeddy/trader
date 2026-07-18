package review

// Thresholds holds every triage gate and threshold used by Classify and
// ReviewPair. Values here were previously hardcoded constants/literals in
// classify.go and pair.go; moving them into a struct lets callers vary them
// (config file, CLI flags) without recompiling — see GitHub issue #165.
type Thresholds struct {
	// HotD1ADXFloor and HotD1CICeiling gate the Hot bucket: D1 ADX must be
	// at or above the floor, D1 CI strictly below the ceiling.
	HotD1ADXFloor  float64
	HotD1CICeiling float64

	// TradeableH4CICeiling gates the Tradeable bucket: H4 CI must be
	// strictly below this ceiling.
	TradeableH4CICeiling float64

	// H4ADXFloor and H4MinEMASep are the Tradeable gate's consolidation
	// guard (see classify.go doc comment for the false-positive history
	// behind these two). Also drive demotion notes.
	H4ADXFloor  float64
	H4MinEMASep float64

	// DemotionD1ADXFloor and DemotionD1CICeiling drive the D1 demotion
	// notes (informational, not hard gates).
	DemotionD1ADXFloor  float64
	DemotionD1CICeiling float64

	// WeekUsedCaution is the demotion-note threshold for weekly ATR
	// consumption.
	WeekUsedCaution float64

	// ValueZoneMin and ValueZoneMax bound the H4 price-vs-EMA20 (in ATR
	// multiples) "value zone" used by SetupSnapshot.InValueZone.
	ValueZoneMin float64
	ValueZoneMax float64

	// H4SqueezeWidthATR is the H4 Bollinger-width (in ATR multiples)
	// threshold below which H4Snapshot.Squeeze is true.
	H4SqueezeWidthATR float64
}

// DefaultThresholds returns the values that were previously hardcoded
// constants/literals, so behavior is unchanged when no config or flags
// override them.
func DefaultThresholds() Thresholds {
	return Thresholds{
		HotD1ADXFloor:        25.0,
		HotD1CICeiling:       55.0,
		TradeableH4CICeiling: 60.0,
		H4ADXFloor:           20.0,
		H4MinEMASep:          0.3,
		DemotionD1ADXFloor:   20.0,
		DemotionD1CICeiling:  65.0,
		WeekUsedCaution:      0.90,
		ValueZoneMin:         0.5,
		ValueZoneMax:         1.5,
		H4SqueezeWidthATR:    2.0,
	}
}

// MergeThresholds returns base with every zero-valued field replaced by the
// corresponding field from override. Use this to layer config-file values
// over defaults, and CLI-flag values over that: the zero value of float64
// means "not configured," consistent with how GlobalConfig treats an empty
// string as unset.
func MergeThresholds(base, override Thresholds) Thresholds {
	if override.HotD1ADXFloor != 0 {
		base.HotD1ADXFloor = override.HotD1ADXFloor
	}
	if override.HotD1CICeiling != 0 {
		base.HotD1CICeiling = override.HotD1CICeiling
	}
	if override.TradeableH4CICeiling != 0 {
		base.TradeableH4CICeiling = override.TradeableH4CICeiling
	}
	if override.H4ADXFloor != 0 {
		base.H4ADXFloor = override.H4ADXFloor
	}
	if override.H4MinEMASep != 0 {
		base.H4MinEMASep = override.H4MinEMASep
	}
	if override.DemotionD1ADXFloor != 0 {
		base.DemotionD1ADXFloor = override.DemotionD1ADXFloor
	}
	if override.DemotionD1CICeiling != 0 {
		base.DemotionD1CICeiling = override.DemotionD1CICeiling
	}
	if override.WeekUsedCaution != 0 {
		base.WeekUsedCaution = override.WeekUsedCaution
	}
	if override.ValueZoneMin != 0 {
		base.ValueZoneMin = override.ValueZoneMin
	}
	if override.ValueZoneMax != 0 {
		base.ValueZoneMax = override.ValueZoneMax
	}
	if override.H4SqueezeWidthATR != 0 {
		base.H4SqueezeWidthATR = override.H4SqueezeWidthATR
	}
	return base
}
