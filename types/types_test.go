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

// ===== Scale conversions =====

func TestScale6_Conversions(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int32(100_000), PriceScale.Int32())
	assert.Equal(t, int64(100_000), PriceScale.Int64())
}

func TestScale7_Conversions(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int32(1_000_000), MoneyScale.Int32())
	assert.Equal(t, int64(1_000_000), MoneyScale.Int64())
}

// ===== Money.String =====

func TestMoney_String(t *testing.T) {
	t.Parallel()

	// Money.String formats the raw int64 value as a float (not divided by scale).
	m := MoneyFromFloat(1.5) // = Money(1_500_000)
	assert.Equal(t, "1500000.000000", m.String())
	assert.Equal(t, "0.000000", Money(0).String())
}

// ===== FormatNumber =====

func TestFormatNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		price Price
		scale int32
		want  string
	}{
		{"typical_forex", PriceFromFloat(1.23456), int32(PriceScale), "1.23456"},
		{"zero", Price(0), int32(PriceScale), "0.00000"},
		{"whole", PriceFromFloat(2.0), int32(PriceScale), "2.00000"},
		{"scale_1", Price(15), 10, "1.5"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, FormatNumber(tt.price, tt.scale))
		})
	}
}

// ===== ParsePrice =====

func TestParsePrice_Valid(t *testing.T) {
	t.Parallel()

	p, err := ParsePrice("123456")
	require.NoError(t, err)
	assert.Equal(t, Price(123456), p)
}

func TestParsePrice_WithWhitespace(t *testing.T) {
	t.Parallel()

	p, err := ParsePrice("  100  ")
	require.NoError(t, err)
	assert.Equal(t, Price(100), p)
}

func TestParsePrice_Invalid(t *testing.T) {
	t.Parallel()

	_, err := ParsePrice("notanumber")
	assert.Error(t, err)
}

func TestParsePrice_Negative(t *testing.T) {
	t.Parallel()

	p, err := ParsePrice("-50000")
	require.NoError(t, err)
	assert.Equal(t, Price(-50000), p)
}

// ===== Price.String =====

func TestPrice_String(t *testing.T) {
	t.Parallel()

	// Price.String formats the raw int32 value as a float (not divided by scale).
	p := PriceFromFloat(1.5) // = Price(150_000)
	assert.Equal(t, "150000.000000", p.String())
	assert.Equal(t, "0.000000", Price(0).String())
}

// ===== Timeframe =====

func TestTF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  Timeframe
	}{
		{"tf0", TF0},
		{"TF0", TF0},
		{"ticks", Ticks},
		{"TICKS", Ticks},
		{"m1", M1},
		{"M1", M1},
		{"h1", H1},
		{"H1", H1},
		{"d1", D1},
		{"D1", D1},
		{"unknown", TF0},
		{"", TF0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, TF(tt.input))
		})
	}
}

func TestNormalizeTF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"60", "m1"},
		{"3600", "h1"},
		{"86400", "d1"},
		{"m1", "M1"},
		{"h1", "H1"},
		{" M1 ", "M1"},
		{"other", "OTHER"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, NormalizeTF(tt.input))
		})
	}
}

func TestTimeframe_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tf   Timeframe
		want string
	}{
		{TF0, "tf0"},
		{Ticks, "ticks"},
		{M1, "m1"},
		{H1, "h1"},
		{D1, "d1"},
		{Timeframe(9999), "UNKNOWN"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.tf.String())
		})
	}
}

// ===== Timestamp conversions =====

func TestTimestamp_Milli(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Timemilli(1_000_000), Timestamp(1000).Milli())
	assert.Equal(t, Timemilli(0), Timestamp(0).Milli())
}

func TestTimemilli_Sec(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Timestamp(5), Timemilli(5000).Sec())
	assert.Equal(t, Timestamp(0), Timemilli(999).Sec())
}

func TestTimestamp_MS(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Timemilli(5000), Timestamp(5).MS())
	assert.Equal(t, Timemilli(0), Timestamp(0).MS())
}

func TestTimestamp_FloorToMinute(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Timestamp(120), Timestamp(125).FloorToMinute())
	assert.Equal(t, Timestamp(0), Timestamp(59).FloorToMinute())
	assert.Equal(t, Timestamp(3600), Timestamp(3600).FloorToMinute())
}

func TestTimestamp_FloorToHour(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Timestamp(3600), Timestamp(3700).FloorToHour())
	assert.Equal(t, Timestamp(0), Timestamp(3599).FloorToHour())
	assert.Equal(t, Timestamp(7200), Timestamp(7200).FloorToHour())
}

func TestTimemilli_FloorToMinute(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Timemilli(120_000), Timemilli(125_000).FloorToMinute())
	assert.Equal(t, Timemilli(0), Timemilli(59_999).FloorToMinute())
}

func TestTimemilli_FloorToHour(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Timemilli(3_600_000), Timemilli(3_700_000).FloorToHour())
	assert.Equal(t, Timemilli(0), Timemilli(3_599_999).FloorToHour())
}

func TestTimeMilliFromTime(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	ms := TimeMilliFromTime(t0)
	assert.Equal(t, Timemilli(t0.UnixMilli()), ms)
}

// ===== DaysInMonth =====

func TestDaysInMonth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		year   int
		month0 int // 0-indexed
		want   int
	}{
		{2023, 0, 31},  // January
		{2023, 1, 28},  // February (non-leap)
		{2024, 1, 29},  // February (leap)
		{2023, 3, 30},  // April
		{2023, 11, 31}, // December
	}

	for _, tt := range tests {
		tt := tt
		t.Run(fmt.Sprintf("%d-%02d", tt.year, tt.month0+1), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, DaysInMonth(tt.year, tt.month0))
		})
	}
}

// ===== TimeRange =====

func TestNewTimeRange(t *testing.T) {
	t.Parallel()

	start, end := Timestamp(1000), Timestamp(2000)
	tr := NewTimeRange(start, end)
	assert.Equal(t, start, tr.Start)
	assert.Equal(t, end, tr.End)
}

func TestTimeRange_Valid(t *testing.T) {
	t.Parallel()

	assert.True(t, NewTimeRange(Timestamp(1000), Timestamp(2000)).Valid())
	assert.False(t, NewTimeRange(Timestamp(0), Timestamp(2000)).Valid(), "start=0 is invalid")
	assert.False(t, NewTimeRange(Timestamp(2000), Timestamp(1000)).Valid(), "end<=start is invalid")
	assert.False(t, NewTimeRange(Timestamp(1000), Timestamp(1000)).Valid(), "end==start is invalid")
}

func TestTimeRange_Contains(t *testing.T) {
	t.Parallel()

	tr := NewTimeRange(Timestamp(100), Timestamp(200))
	assert.True(t, tr.Contains(Timestamp(100)), "start is inclusive")
	assert.True(t, tr.Contains(Timestamp(150)))
	assert.False(t, tr.Contains(Timestamp(200)), "end is exclusive")
	assert.False(t, tr.Contains(Timestamp(50)))
	assert.False(t, tr.Contains(Timestamp(250)))
}

func TestTimeRange_Overlaps(t *testing.T) {
	t.Parallel()

	a := NewTimeRange(Timestamp(100), Timestamp(200))
	assert.True(t, a.Overlaps(NewTimeRange(Timestamp(150), Timestamp(250))), "partial overlap")
	assert.True(t, a.Overlaps(NewTimeRange(Timestamp(50), Timestamp(150))), "overlaps on left")
	assert.False(t, a.Overlaps(NewTimeRange(Timestamp(200), Timestamp(300))), "adjacent does not overlap")
	assert.False(t, a.Overlaps(NewTimeRange(Timestamp(50), Timestamp(100))), "adjacent on left")
}

func TestTimeRange_Covers(t *testing.T) {
	t.Parallel()

	outer := NewTimeRange(Timestamp(100), Timestamp(300))
	inner := NewTimeRange(Timestamp(150), Timestamp(250))
	partial := NewTimeRange(Timestamp(50), Timestamp(250))

	assert.True(t, outer.Covers(inner), "outer covers inner")
	assert.True(t, outer.Covers(outer), "range covers itself")
	assert.False(t, outer.Covers(partial), "partial overlap is not covered")
	assert.False(t, inner.Covers(outer), "inner cannot cover outer")
}

func TestTimeRange_String(t *testing.T) {
	t.Parallel()

	tr := NewTimeRange(Timestamp(0), Timestamp(3600))
	s := tr.String()
	assert.Contains(t, s, "1970-01-01T00:00:00Z")
	assert.Contains(t, s, "1970-01-01T01:00:00Z")
}

// ===== MonthRange / YearRange =====

func TestMonthRange(t *testing.T) {
	t.Parallel()

	tr := MonthRange(2023, 1) // January 2023
	wantStart := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, Timestamp(wantStart.Unix()), tr.Start)
	assert.Equal(t, Timestamp(wantEnd.Unix()), tr.End)
}

func TestYearRange(t *testing.T) {
	t.Parallel()

	tr := YearRange(2023)
	wantStart := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, Timestamp(wantStart.Unix()), tr.Start)
	assert.Equal(t, Timestamp(wantEnd.Unix()), tr.End)
}

func TestTimeRange_MonthsInRange(t *testing.T) {
	t.Parallel()

	t.Run("single_month", func(t *testing.T) {
		t.Parallel()
		tr := MonthRange(2023, 1)
		months := tr.MonthsInRange()
		assert.Equal(t, []YearMonth{{Year: 2023, Month: 1}}, months)
	})

	t.Run("invalid_range", func(t *testing.T) {
		t.Parallel()
		invalid := NewTimeRange(Timestamp(0), Timestamp(0))
		assert.Nil(t, invalid.MonthsInRange())
	})

	t.Run("multi_month", func(t *testing.T) {
		t.Parallel()
		tr := TimeRange{
			Start: Timestamp(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).Unix()),
			End:   Timestamp(time.Date(2023, 3, 15, 0, 0, 0, 0, time.UTC).Unix()),
		}
		months := tr.MonthsInRange()
		require.Len(t, months, 3)
		assert.Equal(t, YearMonth{2023, 1}, months[0])
		assert.Equal(t, YearMonth{2023, 2}, months[1])
		assert.Equal(t, YearMonth{2023, 3}, months[2])
	})

	t.Run("cross_year", func(t *testing.T) {
		t.Parallel()
		tr := TimeRange{
			Start: Timestamp(time.Date(2022, 11, 1, 0, 0, 0, 0, time.UTC).Unix()),
			End:   Timestamp(time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC).Unix()),
		}
		months := tr.MonthsInRange()
		require.Len(t, months, 3)
		assert.Equal(t, YearMonth{2022, 11}, months[0])
		assert.Equal(t, YearMonth{2022, 12}, months[1])
		assert.Equal(t, YearMonth{2023, 1}, months[2])
	})
}

// ===== IsForexMarketClosed / isMajorForexHolidayClosed =====

func TestIsForexMarketClosed(t *testing.T) {
	t.Parallel()

	nyLoc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	tests := []struct {
		name   string
		t      time.Time
		closed bool
	}{
		// Saturday - always closed
		{
			"saturday_noon",
			time.Date(2023, 6, 3, 12, 0, 0, 0, nyLoc),
			true,
		},
		// Sunday before 17:00 - closed
		{
			"sunday_before_17",
			time.Date(2023, 6, 4, 12, 0, 0, 0, nyLoc),
			true,
		},
		// Sunday at 17:00 - open
		{
			"sunday_at_17",
			time.Date(2023, 6, 4, 17, 0, 0, 0, nyLoc),
			false,
		},
		// Sunday after 17:00 - open
		{
			"sunday_after_17",
			time.Date(2023, 6, 4, 20, 0, 0, 0, nyLoc),
			false,
		},
		// Friday before 17:00 - open
		{
			"friday_before_17",
			time.Date(2023, 6, 2, 12, 0, 0, 0, nyLoc),
			false,
		},
		// Friday at 17:00 - closed
		{
			"friday_at_17",
			time.Date(2023, 6, 2, 17, 0, 0, 0, nyLoc),
			true,
		},
		// Normal Monday - open
		{
			"monday_normal",
			time.Date(2023, 6, 5, 12, 0, 0, 0, nyLoc),
			false,
		},
		// January 1 on a weekday - closed (Jan 1, 2024 is Monday)
		{
			"new_years_day_weekday",
			time.Date(2024, 1, 1, 12, 0, 0, 0, nyLoc),
			true,
		},
		// December 25 on a weekday - closed (Dec 25, 2023 is Monday)
		{
			"christmas_weekday",
			time.Date(2023, 12, 25, 12, 0, 0, 0, nyLoc),
			true,
		},
		// December 24 before 13:00 on weekday - open (Dec 24, 2024 is Tuesday)
		{
			"christmas_eve_before_13",
			time.Date(2024, 12, 24, 12, 0, 0, 0, nyLoc),
			false,
		},
		// December 24 at 13:00 on weekday - closed
		{
			"christmas_eve_at_13",
			time.Date(2024, 12, 24, 13, 0, 0, 0, nyLoc),
			true,
		},
		// December 31 before 13:00 on weekday - open (Dec 31, 2024 is Tuesday)
		{
			"new_years_eve_before_13",
			time.Date(2024, 12, 31, 12, 0, 0, 0, nyLoc),
			false,
		},
		// December 31 at 13:00 on weekday - closed
		{
			"new_years_eve_at_13",
			time.Date(2024, 12, 31, 13, 0, 0, 0, nyLoc),
			true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.closed, IsForexMarketClosed(tt.t), "IsForexMarketClosed(%s)", tt.t)
		})
	}
}

// ===== MulDivFloor64 =====

func TestMulDivFloor64_BasicComputation(t *testing.T) {
	t.Parallel()

	// 10 * 20 / 4 = 50 exactly (no remainder, floor == result)
	got, err := MulDivFloor64(10, 20, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(50), got)
}

func TestMulDivFloor64_FloorRounding(t *testing.T) {
	t.Parallel()

	// 10 * 3 / 4 = 7.5 → floor = 7
	got, err := MulDivFloor64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(7), got)
}

func TestMulDivFloor64_InvalidArgs(t *testing.T) {
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
			_, err := MulDivFloor64(tt.a, tt.b, tt.d)
			assert.Error(t, err)
		})
	}
}

func TestMulDivFloor64_Overflow(t *testing.T) {
	t.Parallel()

	_, err := MulDivFloor64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err, "result exceeding MaxInt64 must return an overflow error")
}

// ===== MulDivCeil64 =====

func TestMulDivCeil64_BasicComputation(t *testing.T) {
	t.Parallel()

	// 10 * 20 / 4 = 50 exactly
	got, err := MulDivCeil64(10, 20, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(50), got)
}

func TestMulDivCeil64_CeilingRounding(t *testing.T) {
	t.Parallel()

	// 10 * 3 / 4 = 7.5 → ceil = 8
	got, err := MulDivCeil64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), got)
}

func TestMulDivCeil64_InvalidArgs(t *testing.T) {
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
			_, err := MulDivCeil64(tt.a, tt.b, tt.d)
			assert.Error(t, err)
		})
	}
}

func TestMulDivCeil64_Overflow(t *testing.T) {
	t.Parallel()

	_, err := MulDivCeil64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err, "result exceeding MaxInt64 must return an overflow error")
}

// ===== Pips =====

func TestPipsFromFloat_Scaling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input float64
		want  Pips
	}{
		{"zero", 0.0, 0},
		{"one_pip", 1.0, Pips(PipScale)},
		{"half_pip", 0.5, Pips(PipScale / 2)},
		{"two_pips", 2.0, Pips(2 * PipScale)},
		{"negative", -1.5, Pips(-15)},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, PipsFromFloat(tt.input))
		})
	}
}

func TestPips_Float64_RoundTrip(t *testing.T) {
	t.Parallel()

	cases := []float64{0.0, 1.0, 0.5, 2.0, -1.5, 10.0}
	for _, v := range cases {
		p := PipsFromFloat(v)
		got := p.Float64()
		assert.InDelta(t, v, got, 1e-9, "round-trip failed for %f", v)
	}
}

// ===== Units.Int64 =====

func TestUnits_Int64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input Units
		want  int64
	}{
		{0, 0},
		{1, 1},
		{-100, -100},
		{1_000_000, 1_000_000},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.input.Int64())
		})
	}
}

// ===== Side =====

func TestSide_Constants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Side(-1), Short)
	assert.Equal(t, Side(1), Long)
	assert.NotEqual(t, Short, Long)
}

// ===== GapKind / Gap =====

func TestGapKind_Constants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, GapKind("minor"), GapMinor)
	assert.Equal(t, GapKind("weekend"), GapWeekend)
	assert.Equal(t, GapKind("suspicious"), GapSuspicious)
}

func TestGap_Fields(t *testing.T) {
	t.Parallel()

	g := Gap{
		Start: Timestamp(1000),
		End:   Timestamp(2000),
		TF:    M1,
		Kind:  GapWeekend,
	}

	assert.Equal(t, Timestamp(1000), g.Start)
	assert.Equal(t, Timestamp(2000), g.End)
	assert.Equal(t, M1, g.TF)
	assert.Equal(t, GapWeekend, g.Kind)
}
