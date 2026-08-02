package numericstudy

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Scale is a candidate fixed internal representation scale for Price.
//
// A decimal value v is stored as the int64 round(v * Factor), where Factor is
// exactly 10**Decimals.  The scale is a property of the representation, not of
// any instrument: instrument tick size, permitted increment, and display
// precision are separate concerns (see TickSize on Asset and FormatFixed).
type Scale struct {
	Name     string
	Decimals int
	Factor   int64
}

// Candidates are the internal scales evaluated by this study.
var Candidates = []Scale{
	{Name: "1e5", Decimals: 5, Factor: 100_000},
	{Name: "1e6", Decimals: 6, Factor: 1_000_000},
	{Name: "1e8", Decimals: 8, Factor: 100_000_000},
	{Name: "1e9", Decimals: 9, Factor: 1_000_000_000},
}

// Parse errors.  They are distinct so the study can report *why* a
// representative value fails at a candidate scale: losing precision and
// exceeding range lead to different conclusions.
var (
	// ErrTooManyDecimals means the value carries more significant fraction
	// digits than the scale can hold.  The value is rejected rather than
	// rounded: this study must surface inexact values, not hide them.
	ErrTooManyDecimals = errors.New("numericstudy: more fraction digits than scale")

	// ErrOverflow means the scaled value does not fit in int64.
	ErrOverflow = errors.New("numericstudy: scaled value overflows int64")

	// ErrSyntax means the text is not a plain decimal number.
	ErrSyntax = errors.New("numericstudy: malformed decimal text")
)

// ParseDecimal converts plain decimal text to a scaled int64 without passing
// through binary floating point.  Accepted syntax is an optional '-' followed
// by digits, optionally followed by '.' and more digits.  Exponents, grouping
// separators, leading '+', and surrounding whitespace are rejected: exact
// boundaries should be strict, and provider-specific formats belong in an
// adapter (see Asset.Note for quotation conventions this cannot represent).
func ParseDecimal(s string, sc Scale) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("%w: empty", ErrSyntax)
	}

	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
		if s == "" {
			return 0, fmt.Errorf("%w: bare sign", ErrSyntax)
		}
	}

	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i+1:]
		if strings.ContainsRune(fracPart, '.') {
			return 0, fmt.Errorf("%w: multiple decimal points in %q", ErrSyntax, s)
		}
		if fracPart == "" {
			return 0, fmt.Errorf("%w: trailing decimal point in %q", ErrSyntax, s)
		}
	}
	if intPart == "" {
		return 0, fmt.Errorf("%w: missing integer digits in %q", ErrSyntax, s)
	}
	if err := checkDigits(intPart); err != nil {
		return 0, err
	}
	if err := checkDigits(fracPart); err != nil {
		return 0, err
	}

	// Trailing fraction zeros carry no value, so "750000.00" is exactly
	// representable at every scale.  Trim before the precision check.
	significant := strings.TrimRight(fracPart, "0")
	if len(significant) > sc.Decimals {
		return 0, fmt.Errorf("%w: %q needs %d decimals, scale %s holds %d",
			ErrTooManyDecimals, s, len(significant), sc.Name, sc.Decimals)
	}

	// Right-pad the fraction to the scale so int+frac is the scaled integer
	// in base 10; then accumulate digit by digit with an overflow guard.
	digits := intPart + significant + strings.Repeat("0", sc.Decimals-len(significant))

	var v int64
	for i := 0; i < len(digits); i++ {
		d := int64(digits[i] - '0')
		if v > (math.MaxInt64-d)/10 {
			return 0, fmt.Errorf("%w: %q at scale %s", ErrOverflow, s, sc.Name)
		}
		v = v*10 + d
	}

	if neg {
		v = -v
	}
	return v, nil
}

func checkDigits(s string) error {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return fmt.Errorf("%w: non-digit %q", ErrSyntax, s[i])
		}
	}
	return nil
}

// FormatDecimal renders a scaled int64 as canonical decimal text: no trailing
// fraction zeros, no trailing decimal point, and a single "0" for zero.  This
// is the representation-level form; use FormatFixed for display precision.
func FormatDecimal(v int64, sc Scale) string {
	neg := v < 0

	// Negate through uint64 so math.MinInt64 does not wrap.
	var mag uint64
	if neg {
		mag = uint64(-(v + 1)) + 1
	} else {
		mag = uint64(v)
	}

	factor := uint64(sc.Factor)
	whole := mag / factor
	frac := mag % factor

	out := fmt.Sprintf("%d", whole)
	if frac != 0 {
		f := fmt.Sprintf("%0*d", sc.Decimals, frac)
		out += "." + strings.TrimRight(f, "0")
	}
	if neg && (whole != 0 || frac != 0) {
		out = "-" + out
	}
	return out
}

// FormatFixed renders a scaled int64 with exactly decimals fraction digits,
// truncating toward zero when decimals is smaller than the scale.  It exists
// to demonstrate that instrument display precision is independent of the
// internal representation scale; truncation here is a study convenience, not a
// rounding policy (that belongs to #38).
func FormatFixed(v int64, sc Scale, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}

	neg := v < 0
	var mag uint64
	if neg {
		mag = uint64(-(v + 1)) + 1
	} else {
		mag = uint64(v)
	}

	factor := uint64(sc.Factor)
	whole := mag / factor
	frac := mag % factor

	out := fmt.Sprintf("%d", whole)
	if decimals > 0 {
		f := fmt.Sprintf("%0*d", sc.Decimals, frac)
		if decimals <= len(f) {
			f = f[:decimals]
		} else {
			f += strings.Repeat("0", decimals-len(f))
		}
		out += "." + f
	}
	if neg {
		out = "-" + out
	}
	return out
}

// Canonical returns the canonical decimal text for s, independent of any
// scale: trailing fraction zeros and redundant leading zeros are removed.
// Round-trip tests compare FormatDecimal(ParseDecimal(s)) against Canonical(s)
// rather than against s itself, because "750000.00" and "750000" denote the
// same value.
func Canonical(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}

	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i+1:]
	}

	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	fracPart = strings.TrimRight(fracPart, "0")

	out := intPart
	if fracPart != "" {
		out += "." + fracPart
	}
	if neg && out != "0" {
		out = "-" + out
	}
	return out
}

// MaxPrice returns the largest whole price representable at sc.
func MaxPrice(sc Scale) int64 { return math.MaxInt64 / sc.Factor }

// MaxPriceText returns MaxPrice as decimal text.
func MaxPriceText(sc Scale) string { return fmt.Sprintf("%d", MaxPrice(sc)) }

// Headroom returns how many times larger MaxPrice is than the given scaled
// price.  It answers "how much range is left above this asset" and is 0 when
// the price already exceeds the representable maximum.
func Headroom(scaled int64, sc Scale) int64 {
	if scaled <= 0 {
		return 0
	}
	return math.MaxInt64 / scaled
}

// MulOverflows reports whether a*b overflows int64.  Scaled multiplication
// (Price x Quantity, Price x Rate) forms a double-scaled intermediate before
// the descale divide, so the intermediate — not the result — is what
// constrains the usable scale.
func MulOverflows(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	if a == math.MinInt64 || b == math.MinInt64 {
		return true
	}
	p := a * b
	return p/b != a
}

// AddOverflows reports whether a+b overflows int64.  Used for the rolling-sum
// accumulation case.
func AddOverflows(a, b int64) bool {
	if b > 0 && a > math.MaxInt64-b {
		return true
	}
	if b < 0 && a < math.MinInt64-b {
		return true
	}
	return false
}
