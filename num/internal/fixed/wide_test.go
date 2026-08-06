package fixed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMulScaled(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr error
	}{
		{name: "zero times anything", a: 0, b: 5 * Scale, want: 0},
		{name: "anything times zero", a: 5 * Scale, b: 0, want: 0},
		{name: "identity", a: 7 * Scale, b: Scale, want: 7 * Scale},
		{name: "negative identity", a: -7 * Scale, b: Scale, want: -7 * Scale},
		{name: "two negatives give a positive", a: -3 * Scale, b: -4 * Scale, want: 12 * Scale},
		{name: "mixed signs give a negative", a: -3 * Scale, b: 4 * Scale, want: -12 * Scale},

		// Realistic Price * Rate: 1.08473 marked up by 1.05.
		{name: "price times rate", a: 108_473_000, b: 105_000_000, want: 113_896_650},

		// Realistic Quantity * Rate: 1000 units at a 0.005 financing rate.
		{name: "quantity times rate", a: 1000 * Scale, b: 500_000, want: 5 * Scale},

		// Realistic Price * Quantity, which needs the widened intermediate:
		// the raw product of 1.08473 and 1,000,000 units exceeds int64 by four
		// orders of magnitude before the scale is restored.
		{name: "price times large quantity", a: 108_473_000, b: 1_000_000 * Scale, want: 1_084_730 * Scale},

		// The full negative range is reachable; its positive counterpart is
		// one quantum beyond the representable maximum.
		{name: "reaches the minimum exactly", a: MinRaw, b: Scale, want: MinRaw},
		{name: "reaches the maximum exactly", a: MaxRaw, b: Scale, want: MaxRaw},
		{name: "negating the minimum overflows", a: MinRaw, b: -Scale, wantErr: ErrOverflow},

		{name: "overflows past the maximum", a: MaxRaw, b: 2 * Scale, wantErr: ErrOverflow},
		{name: "underflows past the minimum", a: MinRaw, b: 2 * Scale, wantErr: ErrUnderflow},
		{name: "quotient wider than 64 bits overflows", a: MaxRaw, b: MaxRaw, wantErr: ErrOverflow},
		{name: "negative quotient wider than 64 bits underflows", a: MinRaw, b: MaxRaw, wantErr: ErrUnderflow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MulScaled(tt.a, tt.b, RoundHalfEven)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Zero(t, got, "failed operations must not return a partial result")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMulScaledIsCommutative(t *testing.T) {
	values := []int64{0, 1, -1, Scale, -Scale, 108_473_000, -250_000_000, 3 * Scale}

	for _, a := range values {
		for _, b := range values {
			ab, errAB := MulScaled(a, b, RoundHalfEven)
			ba, errBA := MulScaled(b, a, RoundHalfEven)
			assert.Equal(t, errAB, errBA, "MulScaled(%d,%d) and its reverse disagree on error", a, b)
			assert.Equal(t, ab, ba, "MulScaled(%d,%d) and its reverse disagree on result", a, b)
		}
	}
}

func TestDivScaled(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr error
	}{
		{name: "zero divided by anything", a: 0, b: 5 * Scale, want: 0},
		{name: "identity", a: 7 * Scale, b: Scale, want: 7 * Scale},
		{name: "exact halving", a: 3 * Scale, b: 2 * Scale, want: 150_000_000},
		{name: "negative dividend", a: -3 * Scale, b: 2 * Scale, want: -150_000_000},
		{name: "negative divisor", a: 3 * Scale, b: -2 * Scale, want: -150_000_000},
		{name: "two negatives give a positive", a: -3 * Scale, b: -2 * Scale, want: 150_000_000},

		// Repeating decimals must round at the last representable place, not
		// truncate: 1/3 rounds down, 2/3 rounds up.
		{name: "one third rounds down", a: Scale, b: 3 * Scale, want: 33_333_333},
		{name: "two thirds rounds up", a: 2 * Scale, b: 3 * Scale, want: 66_666_667},

		// Realistic Price / Price producing a Rate.
		{name: "price ratio", a: 108_473_000, b: 108_000_000, want: 100_437_963},

		{name: "divide by zero", a: 5 * Scale, b: 0, wantErr: ErrDivideByZero},
		{name: "zero divided by zero", a: 0, b: 0, wantErr: ErrDivideByZero},
		{name: "overflows past the maximum", a: MaxRaw, b: 1, wantErr: ErrOverflow},
		{name: "underflows past the minimum", a: MinRaw, b: 1, wantErr: ErrUnderflow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DivScaled(tt.a, tt.b, RoundHalfEven)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Zero(t, got, "failed operations must not return a partial result")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRoundingPolicies pins the behavior of all four ADR-004 policies at exact
// midpoints, for both signs.  Multiplying by one quantum makes the operand
// itself the value being rounded, so 250_000_000 is the midpoint 2.5.
func TestRoundingPolicies(t *testing.T) {
	const (
		twoAndAHalf = 250_000_000
		oneAndAHalf = 150_000_000
	)

	tests := []struct {
		name string
		a, b int64
		mode Rounding
		want int64
	}{
		{name: "half-even leaves 2.5 at the even 2", a: 1, b: twoAndAHalf, mode: RoundHalfEven, want: 2},
		{name: "half-even lifts 1.5 to the even 2", a: 1, b: oneAndAHalf, mode: RoundHalfEven, want: 2},
		{name: "half-even leaves -2.5 at the even -2", a: -1, b: twoAndAHalf, mode: RoundHalfEven, want: -2},
		{name: "half-even lowers -1.5 to the even -2", a: -1, b: oneAndAHalf, mode: RoundHalfEven, want: -2},

		{name: "toward zero drops 2.5 to 2", a: 1, b: twoAndAHalf, mode: RoundTowardZero, want: 2},
		{name: "toward zero drops 1.5 to 1", a: 1, b: oneAndAHalf, mode: RoundTowardZero, want: 1},
		{name: "toward zero raises -2.5 to -2", a: -1, b: twoAndAHalf, mode: RoundTowardZero, want: -2},
		{name: "toward zero raises -1.5 to -1", a: -1, b: oneAndAHalf, mode: RoundTowardZero, want: -1},

		{name: "down leaves 2.5 at 2", a: 1, b: twoAndAHalf, mode: RoundDown, want: 2},
		{name: "down leaves 1.5 at 1", a: 1, b: oneAndAHalf, mode: RoundDown, want: 1},
		{name: "down pushes -2.5 to -3", a: -1, b: twoAndAHalf, mode: RoundDown, want: -3},
		{name: "down pushes -1.5 to -2", a: -1, b: oneAndAHalf, mode: RoundDown, want: -2},

		{name: "up lifts 2.5 to 3", a: 1, b: twoAndAHalf, mode: RoundUp, want: 3},
		{name: "up lifts 1.5 to 2", a: 1, b: oneAndAHalf, mode: RoundUp, want: 2},
		{name: "up raises -2.5 to -2", a: -1, b: twoAndAHalf, mode: RoundUp, want: -2},
		{name: "up raises -1.5 to -1", a: -1, b: oneAndAHalf, mode: RoundUp, want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MulScaled(tt.a, tt.b, tt.mode)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRoundingNeighbourhood covers the values immediately below, at, and above
// a midpoint.  ADR-004 requires threshold behavior to be tested on both sides
// of the boundary, not only at it.
func TestRoundingNeighbourhood(t *testing.T) {
	tests := []struct {
		name string
		b    int64
		want int64
	}{
		{name: "just below the midpoint rounds down", b: 149_999_999, want: 1},
		{name: "at the midpoint rounds to even", b: 150_000_000, want: 2},
		{name: "just above the midpoint rounds up", b: 150_000_001, want: 2},
		{name: "just below the next midpoint", b: 249_999_999, want: 2},
		{name: "at the next midpoint rounds to even", b: 250_000_000, want: 2},
		{name: "just above the next midpoint", b: 250_000_001, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			positive, err := MulScaled(1, tt.b, RoundHalfEven)
			require.NoError(t, err)
			assert.Equal(t, tt.want, positive)

			negative, err := MulScaled(-1, tt.b, RoundHalfEven)
			require.NoError(t, err)
			assert.Equal(t, -tt.want, negative, "rounding must be symmetric about zero under half-even")
		})
	}
}

func TestRoundingString(t *testing.T) {
	assert.Equal(t, "half-even", RoundHalfEven.String())
	assert.Equal(t, "toward-zero", RoundTowardZero.String())
	assert.Equal(t, "down", RoundDown.String())
	assert.Equal(t, "up", RoundUp.String())
	assert.Equal(t, "unknown", Rounding(99).String())
}

func TestMagnitudeHandlesTheMostNegativeValue(t *testing.T) {
	// The magnitude of MinRaw has no int64 counterpart; representing it
	// unsigned is what lets the full negative range participate in widened
	// arithmetic and parsing.
	assert.Equal(t, uint64(1)<<63, magnitude(MinRaw))
	assert.Equal(t, uint64(MaxRaw), magnitude(MaxRaw))
	assert.Equal(t, uint64(0), magnitude(0))
	assert.Equal(t, uint64(1), magnitude(-1))
}
