package types

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===== Money =====

func TestMoneyFromFloat_Scaling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input float64
		want  Money
	}{
		{"zero", 0.0, 0},
		{"one_dollar", 1.0, Money(MoneyScale)},
		{"half_dollar", 0.5, Money(MoneyScale / 2)},
		{"one_cent", 0.01, Money(MoneyScale / 100)},
		{"negative", -2.5, Money(-2*MoneyScale - MoneyScale/2)},
		{"large", 1_000_000.0, Money(int64(1_000_000) * int64(MoneyScale))},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, MoneyFromFloat(tt.input))
		})
	}
}

func TestMoney_Float64_RoundTrip(t *testing.T) {
	t.Parallel()

	cases := []float64{0.0, 1.0, 1.5, 0.123456, -42.0, 100_000.0}
	for _, v := range cases {
		m := MoneyFromFloat(v)
		got := m.Float64()
		assert.InDelta(t, v, got, 1e-6, "round-trip failed for %f", v)
	}
}

// TestMoney_Overflow verifies that MoneyFromFloat silently wraps when the
// required scaled value exceeds math.MaxInt64 (Money is int64).
func TestMoney_Overflow(t *testing.T) {
	t.Parallel()

	// The maximum float value that can be stored correctly.
	// MoneyScale = 1_000_000, so maxSafe ≈ 9.22e12.
	maxSafe := float64(math.MaxInt64) / float64(MoneyScale)

	// Just below the boundary: positive result expected.
	mSafe := MoneyFromFloat(maxSafe - 1)
	assert.True(t, mSafe > 0, "value just below max should be a large positive Money")

	// Doubling maxSafe requires ~1.84e19 which exceeds math.MaxInt64 (~9.22e18).
	// The float64→int64 cast silently overflows and wraps to a negative value.
	mOverflow := MoneyFromFloat(maxSafe * 2)
	assert.True(t, mOverflow < 0,
		"values exceeding int64 range silently overflow (two's complement wrap to negative)")
}

// ===== Price =====

func TestPriceFromFloat_Scaling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input float64
		want  Price
	}{
		{"zero", 0.0, 0},
		{"one", 1.0, Price(PriceScale)},
		{"typical_forex", 1.2345, Price(123_450)},
		{"minimum_tick", 1.00001, Price(100_001)},
		{"negative", -1.5, Price(-150_000)},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, PriceFromFloat(tt.input))
		})
	}
}

// TestPrice_Overflow verifies the int32 overflow boundary for Price.
// Price is int32 with PriceScale = 1_000_000, so values above ~2147.48 overflow.
func TestPrice_Overflow(t *testing.T) {
	t.Parallel()

	// Exact boundary: math.MaxInt32 / PriceScale ≈ 2147.483647
	maxSafe := float64(math.MaxInt32) / float64(PriceScale)

	// At the boundary the value should be stored correctly.
	pMax := PriceFromFloat(maxSafe)
	assert.Equal(t, Price(math.MaxInt32), pMax,
		"max representable price equals math.MaxInt32")

	// 2200.0 * 1_000_000 = 2_200_000_000 > MaxInt32 (2_147_483_647).
	// Price is int32 with PriceScale = 100_000, so values above ~21474.83647 overflow.
	pOverflow := PriceFromFloat(22000.0)
	expectedInt64 := int64(math.Round(22000.0 * float64(PriceScale)))
	assert.NotEqual(t, expectedInt64, int64(pOverflow),
		"values exceeding int32 range silently overflow and produce an incorrect result")
}

// ===== Rate =====

func TestRateFromFloat_Scaling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input float64
		want  Rate
	}{
		{"zero", 0.0, 0},
		{"one", 1.0, Rate(RateScale)},
		{"half", 0.5, Rate(RateScale / 2)},
		{"minimum", 0.000001, Rate(1)},
		{"negative", -0.25, Rate(-250_000)},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, RateFromFloat(tt.input))
		})
	}
}

func TestRate_Float64_RoundTrip(t *testing.T) {
	t.Parallel()

	cases := []float64{0.0, 1.0, 0.5, 0.123456, -0.75, 100.0}
	for _, v := range cases {
		r := RateFromFloat(v)
		got := r.Float64()
		assert.InDelta(t, v, got, 1e-6, "round-trip failed for %f", v)
	}
}

func TestRate_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input float64
		want  string
	}{
		{0.0, "0.000000"},
		{1.0, "1.000000"},
		{0.123456, "0.123456"},
		{-0.5, "-0.500000"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(fmt.Sprintf("%f", tt.input), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, RateFromFloat(tt.input).String())
		})
	}
}

// ===== MulDiv64 =====

func TestMulDiv64_BasicComputation(t *testing.T) {
	t.Parallel()

	// 10 * 20 / 4 = 50
	got, err := MulDiv64(10, 20, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(50), got)
}

func TestMulDiv64_ExactDivision(t *testing.T) {
	t.Parallel()

	// 12 * 5 / 3 = 20 exactly (no remainder, no ceiling bump)
	got, err := MulDiv64(12, 5, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(20), got)
}

func TestMulDiv64_CeilingRounding(t *testing.T) {
	t.Parallel()

	// 10 * 3 / 4 = 7.5 → ceiling = 8
	got, err := MulDiv64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), got)
}

func TestMulDiv64_InvalidArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		a, b, d int64
	}{
		{"negative_a", -1, 10, 4},
		{"negative_b", 10, -1, 4},
		{"zero_den", 10, 20, 0},
		{"negative_den", 10, 20, -1},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := MulDiv64(tt.a, tt.b, tt.d)
			assert.Error(t, err)
		})
	}
}

// TestMulDiv64_Overflow verifies that the 128-bit overflow guard fires when the
// quotient of the intermediate product exceeds math.MaxInt64.
func TestMulDiv64_Overflow(t *testing.T) {
	t.Parallel()

	// math.MaxInt64 * math.MaxInt64 / 1 >> MaxInt64; should return an error.
	_, err := MulDiv64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err, "result exceeding MaxInt64 must return an overflow error")
}

// ===== Abs64 / generic Abs =====

func TestAbs64(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(5), Abs64(5))
	assert.Equal(t, int64(5), Abs64(-5))
	assert.Equal(t, int64(0), Abs64(0))
	assert.Equal(t, int64(math.MaxInt64), Abs64(math.MaxInt64))

	// Abs64(math.MinInt64) overflows because -MinInt64 == MinInt64 in two's complement.
	// Document the known overflow behaviour.
	assert.Equal(t, int64(math.MinInt64), Abs64(math.MinInt64),
		"Abs64(MinInt64) overflows back to MinInt64 (two's complement wrap)")
}

func TestAbs_Generic(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 5, Abs(5))
	assert.Equal(t, 5, Abs(-5))
	assert.Equal(t, 0, Abs(0))
	assert.Equal(t, float64(3.14), Abs(3.14))
	assert.Equal(t, float64(3.14), Abs(-3.14))
	assert.Equal(t, int32(7), Abs(int32(-7)))
	assert.Equal(t, float32(1.5), Abs(float32(-1.5)))
}

// ===== Units =====

func TestUnits_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input Units
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{-100, "-100"},
		{1_000_000, "1000000"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.input.String())
		})
	}
}

// ===== Timestamp =====

func TestFromTime_RoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	ts := FromTime(now)
	assert.Equal(t, now.Unix(), ts.Int64())
	assert.Equal(t, now.Unix(), ts.Time().Unix())
}

func TestTimestamp_IsZero(t *testing.T) {
	t.Parallel()

	assert.True(t, Timestamp(0).IsZero())
	assert.False(t, Timestamp(1).IsZero())
	assert.False(t, Timestamp(-1).IsZero())
}

func TestTimestamp_Before(t *testing.T) {
	t.Parallel()

	early := Timestamp(100)
	late := Timestamp(200)

	// NOTE: the implementation uses reversed semantics compared to time.Time.
	// t.Before(ts) returns (ts < t), i.e. "does ts precede t?" not "does t precede ts?".
	assert.False(t, late.Before(early), "reversed implementation: late.Before(early) asks whether early precedes late")
	assert.True(t, early.Before(late), "reversed implementation: early.Before(late) asks whether late precedes early")
	assert.False(t, early.Before(early), "equal timestamps: not Before")
}

func TestTimestamp_After(t *testing.T) {
	t.Parallel()

	early := Timestamp(100)
	late := Timestamp(200)

	// NOTE: the implementation uses reversed semantics compared to time.Time.
	// t.After(ts) returns (t < ts), i.e. "does t precede ts?" not "does t follow ts?".
	assert.False(t, early.After(late), "reversed implementation: early.After(late) asks whether early precedes late")
	assert.True(t, late.After(early), "reversed implementation: late.After(early) asks whether late precedes early")
	assert.False(t, early.After(early), "equal timestamps: not After")
}

func TestTimestamp_Add(t *testing.T) {
	t.Parallel()

	base := Timestamp(1_000_000)
	d := 500 * time.Second
	result := base.Add(d)
	assert.Equal(t, Timestamp(1_000_500), result)
}

func TestTimestamp_String(t *testing.T) {
	t.Parallel()

	// Unix epoch should format as 1970-01-01T00:00:00Z in RFC3339.
	epoch := Timestamp(0)
	assert.Equal(t, "1970-01-01T00:00:00Z", epoch.String())
}

// ===== ID =====

func TestNew_GeneratesUniqueIDs(t *testing.T) {
	t.Parallel()

	const n = 100
	ids := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := NewULID()
		require.NotEmpty(t, id)
		_, duplicate := ids[id]
		require.False(t, duplicate, "New() generated a duplicate ID: %s", id)
		ids[id] = struct{}{}
	}
}

// isValidCrockfordChar reports whether ch is in the Crockford base32 alphabet
// (digits 0-9 and uppercase A-Z excluding I, L, O, U).
func isValidCrockfordChar(ch rune) bool {
	return (ch >= '0' && ch <= '9') ||
		(ch >= 'A' && ch <= 'Z' && ch != 'I' && ch != 'L' && ch != 'O' && ch != 'U')
}

func TestNew_ValidULIDFormat(t *testing.T) {
	t.Parallel()

	id := NewULID()
	// A ULID is exactly 26 characters of Crockford base32.
	const ulidLen = 26
	assert.Len(t, id, ulidLen, "ULID must be %d characters", ulidLen)

	// All characters must belong to the Crockford base32 alphabet.
	for _, ch := range id {
		assert.True(t, isValidCrockfordChar(ch),
			"unexpected character %q in ULID %s", ch, id)
	}
}
