package analysis

// Regime classifies the long-term trend regime at one observation bar,
// per docs/research/eqr-01-research-protocol.org: Positive iff
// Close[t] > EMA(200)[t], Non-Positive otherwise.
//
// Regime is a different measurement of a different thing than
// RSIBucket: regime is the long-term trend condition the EQR-01
// hypothesis requires ("in a positive long-term trend"), while
// RSIBucket is the short-term pullback observable ("after unusually
// sharp pullbacks"). The protocol deliberately keeps the two separate
// rather than folding them into one combined classification.
type Regime uint8

const (
	// RegimePositive is Close[t] > EMA(200)[t].
	RegimePositive Regime = iota
	// RegimeNonPositive is Close[t] <= EMA(200)[t].
	RegimeNonPositive
)

// String returns a short, stable, human-readable label for r.
func (r Regime) String() string {
	if r == RegimePositive {
		return "positive"
	}
	return "non_positive"
}

// MarshalText implements encoding.TextMarshaler so Regime serializes as
// its stable string label rather than a bare integer.
func (r Regime) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, the inverse of
// MarshalText. It returns ErrUnknownRegime for any text other than
// String's two labels.
func (r *Regime) UnmarshalText(text []byte) error {
	switch string(text) {
	case "positive":
		*r = RegimePositive
	case "non_positive":
		*r = RegimeNonPositive
	default:
		return ErrUnknownRegime
	}
	return nil
}

// ClassifyRegime returns RegimePositive iff close > ema, RegimeNonPositive
// otherwise (close <= ema, including equality). Callers must not call it
// before the underlying indicator.EMA reports Ready.
func ClassifyRegime(close, ema float64) Regime {
	if close > ema {
		return RegimePositive
	}
	return RegimeNonPositive
}
