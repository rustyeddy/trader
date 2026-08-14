package marketdata

import (
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// p is a test helper turning a decimal string into a num.Price.
func p(t *testing.T, s string) num.Price {
	t.Helper()
	price, err := num.ParsePrice(s)
	require.NoErrorf(t, err, "ParsePrice(%q)", s)
	return price
}

// validBar returns a well-formed bar the tests mutate to exercise
// individual invariants.
func validBar(t *testing.T) Bar {
	t.Helper()
	return Bar{
		Time:      time.Date(2020, 3, 1, 22, 0, 0, 0, time.UTC),
		Open:      p(t, "1.10000"),
		High:      p(t, "1.10250"),
		Low:       p(t, "1.09900"),
		Close:     p(t, "1.10100"),
		AvgSpread: p(t, "0.00012"),
		MaxSpread: p(t, "0.00030"),
		Ticks:     4213,
	}
}

func TestBarValidate_OK(t *testing.T) {
	require.NoError(t, validBar(t).Validate())
}

func TestBarValidate_DojiAllowed(t *testing.T) {
	b := validBar(t)
	flat := p(t, "1.10000")
	b.Open, b.High, b.Low, b.Close = flat, flat, flat, flat
	assert.NoError(t, b.Validate(), "High == Low doji bar must be valid")
}

func TestBarValidate_ZeroValue(t *testing.T) {
	// The zero Bar must be rejected: canonical storage never persists a
	// zero-filled dummy bar for a closed/missing interval.
	err := Bar{}.Validate()
	assert.ErrorIs(t, err, ErrBarTime)
}

func TestBarValidate_ZeroTime(t *testing.T) {
	b := validBar(t)
	b.Time = time.Time{}
	assert.ErrorIs(t, b.Validate(), ErrBarTime)
}

func TestBarValidate_OHLC(t *testing.T) {
	tests := map[string]func(b *Bar){
		"high below low":   func(b *Bar) { b.High = p(t, "1.09800") },
		"open above high":  func(b *Bar) { b.Open = p(t, "1.10300") },
		"open below low":   func(b *Bar) { b.Open = p(t, "1.09800") },
		"close above high": func(b *Bar) { b.Close = p(t, "1.10300") },
		"close below low":  func(b *Bar) { b.Close = p(t, "1.09800") },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			b := validBar(t)
			mutate(&b)
			assert.ErrorIs(t, b.Validate(), ErrBarOHLC)
		})
	}
}

func TestBarValidate_AvgExceedsMax(t *testing.T) {
	b := validBar(t)
	b.AvgSpread = p(t, "0.00040")
	b.MaxSpread = p(t, "0.00030")
	assert.ErrorIs(t, b.Validate(), ErrBarSpread)
}

func TestBarValidate_AvgEqualsMaxAllowed(t *testing.T) {
	b := validBar(t)
	b.AvgSpread = p(t, "0.00030")
	b.MaxSpread = p(t, "0.00030")
	assert.NoError(t, b.Validate())
}

func TestBarValidate_NegativeTicks(t *testing.T) {
	b := validBar(t)
	b.Ticks = -1
	assert.ErrorIs(t, b.Validate(), ErrBarTicks)
}

func TestBarValidate_ZeroTicksAllowed(t *testing.T) {
	b := validBar(t)
	b.Ticks = 0
	assert.NoError(t, b.Validate(), "a genuinely zero tick count is valid")
}

func TestBarMid(t *testing.T) {
	b := validBar(t)
	// Close 1.10100 + AvgSpread/2 (0.00006) = 1.10106.
	mid, err := b.Mid()
	require.NoError(t, err)
	assert.Equal(t, p(t, "1.10106"), mid)
}

func TestBarMid_Overflow(t *testing.T) {
	// A near-maximum Close plus half a huge spread overflows the exact
	// representation; Mid must surface the error rather than wrap around.
	big := p(t, "92000000000")
	b := Bar{
		Time:      time.Date(2020, 3, 1, 22, 0, 0, 0, time.UTC),
		Open:      big,
		High:      big,
		Low:       big,
		Close:     big,
		AvgSpread: p(t, "90000000000"),
		MaxSpread: p(t, "90000000000"),
	}
	_, err := b.Mid()
	assert.Error(t, err)
}

func TestBarRange(t *testing.T) {
	cal := NewFXCalendar(FXCalendarParams{})
	// 2020-03-01T22:00:00Z is Sunday 17:00 America/New_York — the FX
	// week/day open. A D1 bar anchored there spans one trading day.
	b := validBar(t)
	got, err := b.Range(D1, cal)
	require.NoError(t, err)
	assert.True(t, got.Contains(b.Time), "range must contain the bar's own open")
	// Compare instants, not wall-clock representation: FXCalendar returns
	// the aligned open in America/New_York, the same instant as the UTC
	// open the bar was anchored at.
	assert.True(t, got.Start().Equal(b.Time), "D1 range should start at the aligned open")
}

func TestBarRange_NilCalendar(t *testing.T) {
	_, err := validBar(t).Range(D1, nil)
	assert.Error(t, err)
}

func TestPriceBasisString(t *testing.T) {
	assert.Equal(t, "unknown", BasisUnknown.String())
	assert.Equal(t, "bid", BasisBid.String())
	assert.Equal(t, "mid", BasisMid.String())
	assert.Equal(t, "ask", BasisAsk.String())
	assert.Equal(t, "PriceBasis(9)", PriceBasis(9).String())
}

func TestBarValidate_ErrorMessagePrefixed(t *testing.T) {
	err := Bar{}.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marketdata: bar validate:")
	assert.True(t, errors.Is(err, ErrBarTime))
}
