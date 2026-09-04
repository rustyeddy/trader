package analysis

// Bucket identifies one of the five fixed Z-score buckets
// docs/research/mr-01-experiment-definition.org pins before any
// results are examined. Bucket boundaries are not a tunable
// configuration value: they are frozen by MR-01 and implemented here
// exactly as that document states them.
type Bucket uint8

const (
	// BucketExtremeNegative is Z < -2.0.
	BucketExtremeNegative Bucket = iota
	// BucketModerateNegative is -2.0 <= Z < -1.0.
	BucketModerateNegative
	// BucketNeutral is -1.0 <= Z <= 1.0.
	BucketNeutral
	// BucketModeratePositive is 1.0 < Z <= 2.0.
	BucketModeratePositive
	// BucketExtremePositive is Z > 2.0.
	BucketExtremePositive
)

// String returns a short, stable, human-readable label for b. It is
// also used as Bucket's JSON representation (MarshalText/
// UnmarshalText), so results remain both human- and machine-readable
// per issue #280's own requirement.
func (b Bucket) String() string {
	switch b {
	case BucketExtremeNegative:
		return "extreme_negative"
	case BucketModerateNegative:
		return "moderate_negative"
	case BucketNeutral:
		return "neutral"
	case BucketModeratePositive:
		return "moderate_positive"
	case BucketExtremePositive:
		return "extreme_positive"
	default:
		return "unknown"
	}
}

// MarshalText implements encoding.TextMarshaler so Bucket serializes
// as its stable string label rather than a bare integer.
func (b Bucket) MarshalText() ([]byte, error) {
	return []byte(b.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, the inverse of
// MarshalText, so a Bucket serialized by this package round-trips
// through JSON/YAML unchanged. It returns ErrUnknownBucket for any
// text other than one of String's five labels.
func (b *Bucket) UnmarshalText(text []byte) error {
	switch string(text) {
	case "extreme_negative":
		*b = BucketExtremeNegative
	case "moderate_negative":
		*b = BucketModerateNegative
	case "neutral":
		*b = BucketNeutral
	case "moderate_positive":
		*b = BucketModeratePositive
	case "extreme_positive":
		*b = BucketExtremePositive
	default:
		return ErrUnknownBucket
	}
	return nil
}

// Buckets lists every Bucket value in canonical, most-negative-first
// order — the order results should be reported in.
var Buckets = []Bucket{
	BucketExtremeNegative,
	BucketModerateNegative,
	BucketNeutral,
	BucketModeratePositive,
	BucketExtremePositive,
}

// ClassifyZScore returns the Bucket z falls into, per MR-01's fixed
// boundaries:
//
//	Extreme negative:  Z < -2.0
//	Moderate negative: -2.0 <= Z < -1.0
//	Neutral:           -1.0 <= Z <= 1.0
//	Moderate positive:  1.0 < Z <= 2.0
//	Extreme positive:   Z > 2.0
//
// ClassifyZScore has no invalid input: every finite z falls into
// exactly one bucket. Callers are responsible for excluding a
// non-finite or undefined Z-score before calling it (see
// indicator.ZScore.Value's own zero-variance-exclusion contract) —
// ClassifyZScore does not itself check for NaN/Inf.
func ClassifyZScore(z float64) Bucket {
	switch {
	case z < -2.0:
		return BucketExtremeNegative
	case z < -1.0:
		return BucketModerateNegative
	case z <= 1.0:
		return BucketNeutral
	case z <= 2.0:
		return BucketModeratePositive
	default:
		return BucketExtremePositive
	}
}
