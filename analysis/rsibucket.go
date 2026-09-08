package analysis

// RSIBucket identifies one of the six fixed RSI(2) buckets
// docs/research/eqr-01-research-protocol.org pins before any SPY
// result is examined. Bucket boundaries are not a tunable
// configuration value: they are frozen by the EQR-01 protocol and
// implemented here exactly as that document states them.
//
// This is a distinct type from Bucket (MR-01's five Z-score buckets),
// not a reuse of it — see the protocol's own "Relationship to Later
// Issues" note: EQR-01's event-study capability is a new capability,
// reusing the architectural pattern MR-01/MR-03 established, not the
// concrete Z-score-specific types.
type RSIBucket uint8

const (
	// RSIBucketB1 is RSI(2) in [0, 5).
	RSIBucketB1 RSIBucket = iota
	// RSIBucketB2 is RSI(2) in [5, 10).
	RSIBucketB2
	// RSIBucketB3 is RSI(2) in [10, 20).
	RSIBucketB3
	// RSIBucketB4 is RSI(2) in [20, 30).
	RSIBucketB4
	// RSIBucketB5 is RSI(2) in [30, 50).
	RSIBucketB5
	// RSIBucketB6 is RSI(2) in [50, 100].
	RSIBucketB6
)

// String returns a short, stable, human-readable label for b. It is
// also used as RSIBucket's JSON representation (MarshalText/
// UnmarshalText).
func (b RSIBucket) String() string {
	switch b {
	case RSIBucketB1:
		return "B1"
	case RSIBucketB2:
		return "B2"
	case RSIBucketB3:
		return "B3"
	case RSIBucketB4:
		return "B4"
	case RSIBucketB5:
		return "B5"
	case RSIBucketB6:
		return "B6"
	default:
		return "unknown"
	}
}

// MarshalText implements encoding.TextMarshaler so RSIBucket serializes
// as its stable string label rather than a bare integer.
func (b RSIBucket) MarshalText() ([]byte, error) {
	return []byte(b.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, the inverse of
// MarshalText. It returns ErrUnknownRSIBucket for any text other than
// one of String's six labels.
func (b *RSIBucket) UnmarshalText(text []byte) error {
	switch string(text) {
	case "B1":
		*b = RSIBucketB1
	case "B2":
		*b = RSIBucketB2
	case "B3":
		*b = RSIBucketB3
	case "B4":
		*b = RSIBucketB4
	case "B5":
		*b = RSIBucketB5
	case "B6":
		*b = RSIBucketB6
	default:
		return ErrUnknownRSIBucket
	}
	return nil
}

// RSIBuckets lists every RSIBucket value in canonical, lowest-RSI-first
// order — the order results should be reported in.
var RSIBuckets = []RSIBucket{
	RSIBucketB1, RSIBucketB2, RSIBucketB3, RSIBucketB4, RSIBucketB5, RSIBucketB6,
}

// ClassifyRSIBucket returns the RSIBucket rsi falls into, per
// docs/research/eqr-01-research-protocol.org's frozen boundaries,
// inclusive at the lower bound and exclusive at the upper bound except
// B6, which is additionally closed at 100:
//
//	B1: [0, 5)     B2: [5, 10)    B3: [10, 20)
//	B4: [20, 30)   B5: [30, 50)   B6: [50, 100]
//
// ClassifyRSIBucket assumes rsi is already within indicator.RSI's own
// [0, 100] output range (guaranteed once indicator.RSI.Ready is true) —
// callers must not call it before Ready. A value below 0 sorts into B1
// and a value above 100 sorts into B6 rather than being rejected, since
// clamping a slightly out-of-range float is safer than a fabricated
// error for what should be an unreachable input under that precondition.
func ClassifyRSIBucket(rsi float64) RSIBucket {
	switch {
	case rsi < 5:
		return RSIBucketB1
	case rsi < 10:
		return RSIBucketB2
	case rsi < 20:
		return RSIBucketB3
	case rsi < 30:
		return RSIBucketB4
	case rsi < 50:
		return RSIBucketB5
	default:
		return RSIBucketB6
	}
}
