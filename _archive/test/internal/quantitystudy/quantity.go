package quantitystudy

import (
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/test/internal/numericstudy"
)

// Candidates are the internal Quantity scales evaluated by this study.
//
// #36 asks for 1e6 and 1e8; 1e7 and 1e9 are included so the range/precision
// frontier is visible as a curve rather than asserted from two points.
var Candidates = []numericstudy.Scale{
	{Name: "1e6", Decimals: 6, Factor: 1_000_000},
	{Name: "1e7", Decimals: 7, Factor: 10_000_000},
	{Name: "1e8", Decimals: 8, Factor: 100_000_000},
	{Name: "1e9", Decimals: 9, Factor: 1_000_000_000},
}

// PriceScale is the internal Price scale fixed by #33.  Quantity arithmetic is
// evaluated against it, because Price x Quantity is where the two scales meet.
var PriceScale = numericstudy.Scale{Name: "1e8", Decimals: 8, Factor: 100_000_000}

// SelectedScale is the recommended Trader-wide Quantity scale: 1e8, matching
// Price.
//
// It is chosen on precision coverage plus an explicitly bounded supported
// range, not on holding every quantity the pressure matrix contains.  1e8
// represents every intended asset class exactly — FX units, whole and
// fractional equities, futures contracts, and crypto down to one satoshi — and
// supports whole-unit quantities from 0 through MaxSupportedWholeUnits.
//
// Quantities above that bound are rejected as out of range rather than
// silently truncated.  Positions in extremely high-supply tokens above the
// bound are outside the initial supported domain; see the 1e12 row in the
// evidence, which is retained precisely to mark that boundary.
var SelectedScale = numericstudy.Scale{Name: "1e8", Decimals: 8, Factor: 100_000_000}

// MaxSupportedWholeUnits is the largest whole-unit quantity representable at
// SelectedScale: 92,233,720,368, or roughly 92.2 billion units.
var MaxSupportedWholeUnits = numericstudy.MaxPrice(SelectedScale)

// QuantityRules are the instrument and broker constraints on an order
// quantity.  They are deliberately separate from the representation scale: the
// scale says what can be stored, these say what may be traded.
type QuantityRules struct {
	Increment    int64 // smallest tradable step, in scale units; 0 means unconstrained
	Minimum      int64 // smallest permitted order size, in scale units
	Maximum      int64 // largest permitted order size, in scale units; 0 means unbounded
	IntegralOnly bool  // whole units only, e.g. futures contracts
}

// Validation failures.  They are distinct because they carry different
// remedies: a rounding adjustment, a size change, or an outright rejection.
var (
	ErrNegative     = errors.New("quantitystudy: order quantity must not be negative")
	ErrZero         = errors.New("quantitystudy: order quantity must not be zero")
	ErrIncrement    = errors.New("quantitystudy: quantity is not a multiple of the increment")
	ErrNotIntegral  = errors.New("quantitystudy: instrument accepts whole units only")
	ErrBelowMinimum = errors.New("quantitystudy: quantity is below the instrument minimum")
	ErrAboveMaximum = errors.New("quantitystudy: quantity is above the instrument maximum")
)

// Validate checks a scaled quantity against the instrument rules.
//
// Zero is representable and is not rejected here: whether an order may have
// zero quantity is an order-construction question, and Validate is about
// instrument conformance.  ValidateOrder layers the order-level rule on top.
func (r QuantityRules) Validate(q int64, sc numericstudy.Scale) error {
	// Direction is never encoded in the quantity sign; a negative quantity is
	// a programming error, not a sell.
	if q < 0 {
		return fmt.Errorf("%w: %s", ErrNegative, numericstudy.FormatDecimal(q, sc))
	}

	if r.IntegralOnly && q%sc.Factor != 0 {
		return fmt.Errorf("%w: %s", ErrNotIntegral, numericstudy.FormatDecimal(q, sc))
	}

	// Exact integer modulo — no tolerance, no rounding.  This is the whole
	// reason for holding quantity and increment in the same scale.
	if r.Increment > 0 && q%r.Increment != 0 {
		return fmt.Errorf("%w: %s is not a multiple of %s", ErrIncrement,
			numericstudy.FormatDecimal(q, sc), numericstudy.FormatDecimal(r.Increment, sc))
	}

	if r.Minimum > 0 && q < r.Minimum {
		return fmt.Errorf("%w: %s < %s", ErrBelowMinimum,
			numericstudy.FormatDecimal(q, sc), numericstudy.FormatDecimal(r.Minimum, sc))
	}

	if r.Maximum > 0 && q > r.Maximum {
		return fmt.Errorf("%w: %s > %s", ErrAboveMaximum,
			numericstudy.FormatDecimal(q, sc), numericstudy.FormatDecimal(r.Maximum, sc))
	}

	return nil
}

// ValidateOrder adds the order-level rule that a tradable quantity must be
// non-zero.  Keeping it separate from Validate is the point: zero is a
// perfectly representable Quantity — a flat position, a closed lot — and only
// order construction rejects it.
func (r QuantityRules) ValidateOrder(q int64, sc numericstudy.Scale) error {
	if q == 0 {
		return ErrZero
	}
	return r.Validate(q, sc)
}

// MaxWholeUnits reports the largest whole-unit quantity representable at sc.
func MaxWholeUnits(sc numericstudy.Scale) int64 {
	return numericstudy.MaxPrice(sc)
}

// Representable reports whether decimal text is exactly representable at sc.
func Representable(text string, sc numericstudy.Scale) bool {
	_, err := numericstudy.ParseDecimal(text, sc)
	return err == nil
}

// NotionalOverflows reports whether a scaled price times a scaled quantity
// overflows int64 before the descaling divide.
//
// Both operands carry a scale, so the product is double-scaled — the same
// shape as Price x Rate in #33, and the reason #36 feeds back into #38.
func NotionalOverflows(price, qty int64) bool {
	return numericstudy.MulOverflows(price, qty)
}
