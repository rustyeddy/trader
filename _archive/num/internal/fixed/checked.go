package fixed

// Checked int64 arithmetic on raw scaled values.
//
// Every operation here reports failure rather than wrapping.  ADR-004 forbids
// authoritative arithmetic from silently wrapping, saturating, clamping,
// discarding overflow, or escaping range constraints through float64.
//
// Addition and subtraction operate directly on raw scaled values because both
// operands share the common scale: (a*S) + (b*S) == (a+b)*S.  Multiplication
// and division do not have that property and live in wide.go.

// Add returns a+b, or an error if the true sum falls outside int64.
//
// The bound is rearranged rather than computed, so no intermediate ever wraps:
// a+b exceeds maxRaw exactly when a > maxRaw-b, and maxRaw-b is itself
// representable whenever b is positive.
func Add(a, b int64) (int64, error) {
	if b > 0 && a > maxRaw-b {
		return 0, ErrOverflow
	}
	if b < 0 && a < minRaw-b {
		return 0, ErrUnderflow
	}
	return a + b, nil
}

// Sub returns a-b, or an error if the true difference falls outside int64.
//
// Sub is implemented directly rather than as Add(a, -b) because b may be
// minRaw, which cannot be negated.
func Sub(a, b int64) (int64, error) {
	if b < 0 && a > maxRaw+b {
		return 0, ErrOverflow
	}
	if b > 0 && a < minRaw+b {
		return 0, ErrUnderflow
	}
	return a - b, nil
}

// Neg returns -a.
//
// minRaw is rejected: its positive counterpart is not representable in signed
// int64, so negating it would wrap back to itself.
func Neg(a int64) (int64, error) {
	if a == minRaw {
		return 0, ErrNotRepresentable
	}
	return -a, nil
}

// Abs returns the absolute value of a.
//
// minRaw is rejected for the same reason as Neg.
func Abs(a int64) (int64, error) {
	if a == minRaw {
		return 0, ErrNotRepresentable
	}
	if a < 0 {
		return -a, nil
	}
	return a, nil
}

// Sign reports -1, 0, or +1 according to the sign of a.
func Sign(a int64) int {
	switch {
	case a < 0:
		return -1
	case a > 0:
		return 1
	default:
		return 0
	}
}

// Cmp compares two raw scaled values, returning -1, 0, or +1.
//
// Both operands share the common scale, so raw comparison is exact and needs
// no widening.
func Cmp(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
