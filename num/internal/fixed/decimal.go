package fixed

import "math"

// Exact decimal text conversion.
//
// Parsing and formatting both operate directly on the scaled integer
// representation.  Neither routes through float64, which cannot represent most
// decimal values exactly and would silently reintroduce the approximation the
// whole package exists to avoid.

// Parse converts plain decimal text to a raw scaled value.
//
// Accepted input is an optional sign, one or more integer digits, and
// optionally a decimal point followed by one or more fraction digits:
//
//	123        -123        +0.5        0.00000001
//
// Parse rejects, rather than repairing or rounding:
//
//   - empty input, a bare sign, or a bare decimal point;
//   - a missing integer part (".5") or a trailing point ("5.");
//   - more than one sign or more than one decimal point;
//   - exponent notation, which an adapter must expand first;
//   - grouping separators, currency symbols, and locale-dependent forms;
//   - leading, trailing, or embedded whitespace;
//   - significant precision beyond what the places constant supports;
//   - values outside the representable scaled range.
//
// Trailing zeros beyond the places constant's digit count are accepted
// because dropping them is exact and requires no rounding; ADR-004 allows
// readers to accept equivalent exact forms.  A non-zero digit past that
// point reports ErrPrecision, since honouring it would require discarding
// information.
//
// The full signed range is parsable, including the most negative value.  The
// magnitude is accumulated unsigned and the sign applied only at the end, so
// there is no one-value gap where an unrepresentable positive intermediate
// would be constructed on the way to a representable negative result.
func Parse(s string) (int64, error) {
	if s == "" {
		return 0, ErrSyntax
	}

	i := 0
	negative := false
	switch s[0] {
	case '-':
		negative = true
		i++
	case '+':
		i++
	}

	// Integer part: at least one digit, accumulated as a magnitude.
	start := i
	var mag uint64
	for ; i < len(s) && isDigit(s[i]); i++ {
		d := uint64(s[i] - '0')
		if mag > (math.MaxUint64-d)/10 {
			return 0, ErrRange
		}
		mag = mag*10 + d
	}
	if i == start {
		return 0, ErrSyntax
	}

	// Apply the scale to the integer part before adding the fraction, so the
	// two halves are combined in one exactly-scaled magnitude.
	if mag > math.MaxUint64/uint64(scale) {
		return 0, ErrRange
	}
	mag *= uint64(scale)

	if i < len(s) {
		if s[i] != '.' {
			return 0, ErrSyntax
		}
		i++

		start = i
		var frac uint64
		fracDigits := 0
		for ; i < len(s) && isDigit(s[i]); i++ {
			d := uint64(s[i] - '0')
			switch {
			case fracDigits < places:
				frac = frac*10 + d
				fracDigits++
			case d != 0:
				return 0, ErrPrecision
			}
		}
		if i == start {
			return 0, ErrSyntax
		}
		if i != len(s) {
			return 0, ErrSyntax
		}

		// Left-align the fraction to the common scale: "0.5" contributes one
		// place and must become 50000000, not 5.
		for ; fracDigits < places; fracDigits++ {
			frac *= 10
		}

		if mag > math.MaxUint64-frac {
			return 0, ErrRange
		}
		mag += frac
	}

	raw, err := fromMagnitude(mag, negative)
	if err != nil {
		return 0, ErrRange
	}
	return raw, nil
}

// Format renders a raw scaled value as canonical plain decimal text.
//
// Canonical output uses no scientific notation, no grouping separators, no
// unnecessary trailing zeros, and no trailing decimal point.  Negative zero is
// not representable in this integer form, so zero always renders as "0".
// Format and Parse round-trip to the identical raw value.
//
//	12345000000 -> "123.45"
//	  100000000 -> "1"
//	          1 -> "0.00000001"
//	          0 -> "0"
func Format(raw int64) string {
	if raw == 0 {
		return "0"
	}

	mag := magnitude(raw)
	whole := mag / uint64(scale)
	frac := mag % uint64(scale)

	// Longest possible output: sign, 11 integer digits, point, 8 fraction
	// digits.
	buf := make([]byte, 0, 21)
	if raw < 0 {
		buf = append(buf, '-')
	}
	buf = appendUint(buf, whole)

	if frac == 0 {
		return string(buf)
	}

	// Render the fraction zero-padded to the full scale, then trim the
	// trailing zeros that padding and the scale itself introduced.
	var digits [places]byte
	for i := places - 1; i >= 0; i-- {
		digits[i] = byte('0' + frac%10)
		frac /= 10
	}
	end := places
	for end > 1 && digits[end-1] == '0' {
		end--
	}

	buf = append(buf, '.')
	buf = append(buf, digits[:end]...)
	return string(buf)
}

// appendUint appends the decimal digits of v to buf without using strconv, so
// that formatting stays a pure integer operation in one place.
func appendUint(buf []byte, v uint64) []byte {
	if v == 0 {
		return append(buf, '0')
	}

	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	return append(buf, digits[i:]...)
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
