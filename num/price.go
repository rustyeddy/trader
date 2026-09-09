package num

import "github.com/rustyeddy/trader/num/internal/fixed"

// Price represents an authoritative instrument price.
//
// Price is backed by a scaled int64 at the common 1e8 scale (ADR-004). Normal
// domain values are non-negative; constructors and parsers reject negative
// input. Zero is representable — whether zero is a valid price is decided by
// the surrounding context, not by this type.
//
// Tick size, permitted increments, and display precision are instrument
// rules, not properties of Price. Price increment validation is exact when
// price and tick size share this scale: priceRaw % tickRaw == 0.
//
// The zero value of Price is the exact value 0 and is valid.
type Price struct {
	raw int64
}

// ParsePrice parses exact decimal text into a Price.
//
// Negative input is rejected, as are malformed text, excess precision, and
// out-of-range values. Nothing is silently rounded.
func ParsePrice(s string) (Price, error) {
	raw, err := fixed.Parse(s)
	if err != nil {
		return Price{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Price{}, ErrNegative
	}
	return Price{raw: raw}, nil
}

// MustParsePrice is like ParsePrice but panics on error.
//
// It is intended for programmer-controlled constants, fixtures, and tests,
// not for parsing external or market input.
func MustParsePrice(s string) Price {
	p, err := ParsePrice(s)
	if err != nil {
		panic(err)
	}
	return p
}

// String returns p formatted as canonical decimal text.
func (p Price) String() string {
	return fixed.Format(p.raw)
}

// Float64 returns p as a float64, for crossing into Trader's analytical
// domain — indicators and other float64-based calculations (ADR-045,
// docs/arch/adr-045-analytical-float64-conversion-boundary.org).
//
// This is a direct numeric conversion (raw scaled int64 divided by the
// common scale), never a serialize/reparse round-trip through String
// and strconv.ParseFloat: the two are not equivalent in general, and
// ADR-045 explicitly rejects the round-trip form as an implementation
// smell. float64 cannot represent every value Price can exactly (IEEE
// 754 double precision gives roughly 15-17 significant decimal digits,
// against Price's exact 1e8 scale), so this conversion is lossy in the
// same way any exact-to-float64 conversion is. This method itself adds
// no unquantized path back into the exact domain — there is no
// Price-from-float64 constructor here — but ADR-004 already permits a
// strategy's analytical float64 result to become authoritative again
// through the normal checked, quantized, semantically-validated
// construction path (for example rounding to the listing's tick size
// and constructing via ParsePrice/NewPrice), the same as any other
// externally-derived value. ADR-045 does not add that conversion; it
// only adds this one, exact-to-analytical direction.
func (p Price) Float64() float64 {
	scale := fixed.Scale()
	// Split into whole and fractional scaled parts before converting,
	// rather than converting the full raw int64 and dividing: raw can
	// exceed float64's 53-bit exact-integer range (Price's representable
	// range goes well beyond 2^53), and float64(p.raw) alone would round
	// once there, before the division rounds a second time. whole and
	// frac are each always well within 53 bits (frac has at most 8
	// decimal digits by construction), so this decomposition converts
	// each part exactly and only rounds once, at the final addition —
	// the closest float64 to the true decimal value (PR #275 review).
	whole := p.raw / scale
	frac := p.raw % scale
	return float64(whole) + float64(frac)/float64(scale)
}

// IsZero reports whether p is exactly zero.
func (p Price) IsZero() bool {
	return p.raw == 0
}

// Cmp compares p and o, returning -1, 0, or +1.
func (p Price) Cmp(o Price) int {
	return fixed.Cmp(p.raw, o.raw)
}

// Equal reports whether p and o are the exact same value.
func (p Price) Equal(o Price) bool {
	return p.raw == o.raw
}

// Add returns p+o using checked arithmetic.
func (p Price) Add(o Price) (Price, error) {
	raw, err := fixed.Add(p.raw, o.raw)
	if err != nil {
		return Price{}, wrapFixedErr(err)
	}
	return Price{raw: raw}, nil
}

// Sub returns p-o using checked arithmetic.
//
// Sub reports ErrNegative if the true difference would be negative: Price
// has no signed counterpart. Callers that need a signed difference between
// two prices should use a dedicated signed type rather than relax this one.
func (p Price) Sub(o Price) (Price, error) {
	raw, err := fixed.Sub(p.raw, o.raw)
	if err != nil {
		return Price{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Price{}, ErrNegative
	}
	return Price{raw: raw}, nil
}

// MulRate returns p*r, rounded to nearest with ties to even.
//
// MulRate reports ErrNegative if r is negative enough to make the result
// negative.
func (p Price) MulRate(r Rate) (Price, error) {
	raw, err := fixed.MulScaled(p.raw, r.raw, fixed.RoundHalfEven)
	if err != nil {
		return Price{}, wrapFixedErr(err)
	}
	if raw < 0 {
		return Price{}, ErrNegative
	}
	return Price{raw: raw}, nil
}

// Div returns p/o as a dimensionless Rate, rounded to nearest with ties to
// even.
//
// Div reports ErrDivideByZero when o is zero.
func (p Price) Div(o Price) (Rate, error) {
	raw, err := fixed.DivScaled(p.raw, o.raw, fixed.RoundHalfEven)
	if err != nil {
		return Rate{}, wrapFixedErr(err)
	}
	return Rate{raw: raw}, nil
}

// MulQuantity returns the notional value p*q, denominated in currency,
// rounded to nearest with ties to even (ADR-025). Currency is required
// explicitly: neither Price nor Quantity carries a currency of its own,
// so the caller states which currency the resulting Money is
// denominated in — typically the instrument's quote/settlement
// currency, which is not necessarily an account's home currency;
// converting to a different currency is a separate, later step (see
// Money.Convert).
//
// The underlying multiplication uses a widened 128-bit intermediate
// (num/internal/fixed.MulScaled), so a realistic notional value — for
// example a large FX or equity block — never silently overflows int64
// the way a naive scaled-int64 product would; MulQuantity reports
// ErrOverflow instead. Because Price and Quantity are both
// non-negative by construction, the result is always non-negative.
func (p Price) MulQuantity(q Quantity, currency Currency) (Money, error) {
	if !currency.IsValid() {
		return Money{}, ErrMissingCurrency
	}
	raw, err := fixed.MulScaled(p.raw, q.raw, fixed.RoundHalfEven)
	if err != nil {
		return Money{}, wrapFixedErr(err)
	}
	return Money{amount: raw, currency: currency, valid: true}, nil
}

// DivisibleBy reports whether p is an exact integer multiple of step, per
// ADR-004's price-increment rule: priceRaw % tickRaw == 0. The comparison is
// exact scaled-integer arithmetic; no rounding is involved.
//
// DivisibleBy reports ErrDivideByZero when step is zero.
func (p Price) DivisibleBy(step Price) (bool, error) {
	if step.raw == 0 {
		return false, ErrDivideByZero
	}
	return p.raw%step.raw == 0, nil
}

// RoundDown returns the largest multiple of tick that is <= p:
// floor(p/tick) x tick, mirroring Quantity.RoundDown's own scaled-
// integer style exactly (issue #340) — tick.raw and p.raw already
// share the same fixed-point scale, so their plain integer quotient is
// already the exact, dimensionless multiple count, and Go's integer
// division truncates toward zero, which equals floor for the
// non-negative values Price always holds.
//
// RoundDown reports ErrDivideByZero when tick is zero, matching
// DivisibleBy's own existing convention. Callers computing a
// derived price from arithmetic (a fraction of a high-water mark, an
// ATR multiple, and so on) rather than copying an already tick-
// aligned price verbatim must round it to the listing's own
// instrument.Spec.TickSize() before constructing an order.Proposal/
// order.ReplaceRequest — see execution's own IntentAdjustStop handling
// for the one place this is done today. This is an explicit,
// documented rounding step, never an implicit one: num's own package
// doc states plainly that "nothing [is] silently rounded," and
// RoundDown exists precisely so a caller that does want to round has
// a named, exact operation to call instead of reaching for float64
// arithmetic or truncated decimal text.
func (p Price) RoundDown(tick Price) (Price, error) {
	if tick.raw == 0 {
		return Price{}, ErrDivideByZero
	}
	count := p.raw / tick.raw
	return Price{raw: count * tick.raw}, nil
}

// RoundUp returns the smallest multiple of tick that is >= p:
// ceil(p/tick) x tick. See RoundDown's own doc comment for the shared
// rationale and Price's non-negative-by-construction assumption;
// RoundUp differs only in rounding toward positive infinity instead of
// toward zero — the direction needed so a protective Buy stop (which
// protects a short position from above) is never rounded into being
// more aggressive/tighter than what the caller computed: moving it
// further up gives the position strictly more room, never less.
//
// RoundUp reports ErrDivideByZero when tick is zero. A p that is
// already an exact multiple of tick rounds to itself, exactly like
// RoundDown; only a genuinely fractional remainder rounds up to the
// next multiple.
//
// Unlike RoundDown, RoundUp cannot compute its result as a plain
// count*tick.raw product: incrementing the multiple count first and
// then multiplying could overflow int64 for a p near the top of
// Price's representable range, silently wrapping negative and
// violating Price's non-negative invariant (PR #341 review). Instead
// it computes RoundDown's own (always in-range, since it never
// exceeds p.raw) result directly, then adds one further tick via
// fixed.Add's checked arithmetic — reporting ErrOverflow rather than
// wrapping if even that single increment does not fit.
func (p Price) RoundUp(tick Price) (Price, error) {
	if tick.raw == 0 {
		return Price{}, ErrDivideByZero
	}
	down := (p.raw / tick.raw) * tick.raw
	if down == p.raw {
		return Price{raw: down}, nil
	}
	raw, err := fixed.Add(down, tick.raw)
	if err != nil {
		return Price{}, wrapFixedErr(err)
	}
	return Price{raw: raw}, nil
}
