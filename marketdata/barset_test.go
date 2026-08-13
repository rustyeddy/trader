package marketdata

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// barAt returns a valid one-off bar anchored at t for BarSet tests.
func barAt(t *testing.T, at time.Time) Bar {
	t.Helper()
	b := validBar(t)
	b.Time = at
	return b
}

func eurusd() instrument.ID {
	return instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
}

// validBarSet returns a well-formed two-bar H1 set the tests mutate.
func validBarSet(t *testing.T) BarSet {
	t.Helper()
	start := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, 3, 2, 4, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(start, end)
	require.NoError(t, err)
	return BarSet{
		Instrument: eurusd(),
		Interval:   H1,
		Span:       span,
		Basis:      BasisBid,
		Bars: []Bar{
			barAt(t, start),
			barAt(t, start.Add(time.Hour)),
		},
	}
}

func TestBarSetValidate_OK(t *testing.T) {
	require.NoError(t, validBarSet(t).Validate())
}

func TestBarSetValidate_EmptyBarsAllowed(t *testing.T) {
	bs := validBarSet(t)
	bs.Bars = nil
	assert.NoError(t, bs.Validate(), "a span with no observed bars is valid")
	assert.Equal(t, 0, bs.Len())
}

func TestBarSetValidate_ZeroInstrument(t *testing.T) {
	bs := validBarSet(t)
	bs.Instrument = instrument.ID{}
	assert.ErrorIs(t, bs.Validate(), ErrBarSetInstrument)
}

func TestBarSetValidate_ZeroInterval(t *testing.T) {
	bs := validBarSet(t)
	bs.Interval = Interval{}
	assert.ErrorIs(t, bs.Validate(), ErrBarSetInterval)
}

func TestBarSetValidate_ZeroSpan(t *testing.T) {
	bs := validBarSet(t)
	bs.Span = TimeRange{}
	assert.ErrorIs(t, bs.Validate(), ErrBarSetSpan)
}

func TestBarSetValidate_UnknownBasis(t *testing.T) {
	bs := validBarSet(t)
	bs.Basis = BasisUnknown
	assert.ErrorIs(t, bs.Validate(), ErrBarSetBasis)
}

func TestBarSetValidate_InvalidMemberBar(t *testing.T) {
	bs := validBarSet(t)
	bs.Bars[1].Time = time.Time{} // fails Bar.Validate
	assert.ErrorIs(t, bs.Validate(), ErrBarTime)
}

func TestBarSetValidate_BarBeforeSpan(t *testing.T) {
	bs := validBarSet(t)
	bs.Bars[0] = barAt(t, bs.Span.Start().Add(-time.Hour))
	assert.ErrorIs(t, bs.Validate(), ErrBarSetBarRange)
}

func TestBarSetValidate_BarAtSpanEndExcluded(t *testing.T) {
	// Span is half-open: a bar whose Time equals End belongs to the next
	// set, not this one.
	bs := validBarSet(t)
	bs.Bars[1] = barAt(t, bs.Span.End())
	assert.ErrorIs(t, bs.Validate(), ErrBarSetBarRange)
}

func TestBarSetValidate_OutOfOrder(t *testing.T) {
	bs := validBarSet(t)
	bs.Bars[0], bs.Bars[1] = bs.Bars[1], bs.Bars[0]
	assert.ErrorIs(t, bs.Validate(), ErrBarSetOrder)
}

func TestBarSetValidate_DuplicateTimestamp(t *testing.T) {
	bs := validBarSet(t)
	bs.Bars[1] = barAt(t, bs.Bars[0].Time) // same time as bar 0
	assert.ErrorIs(t, bs.Validate(), ErrBarSetOrder)
}
