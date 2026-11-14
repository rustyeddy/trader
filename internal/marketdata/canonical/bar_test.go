package canonical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMidOf_IsTheExactMidpointOfBidAndAsk(t *testing.T) {
	b := goodBar(t, utc(2024, 1, 2, 0))

	m, err := MidOf(b.Bid, b.Ask)

	require.NoError(t, err)
	assert.Equal(t, px("1.10005"), m.Open, "half a pip is exact")
	assert.Equal(t, px("1.101055"), m.High)
	assert.Equal(t, px("1.099055"), m.Low)
	assert.Equal(t, px("1.100555"), m.Close)
	assert.Equal(t, m, b.Mid, "the fixture's mid is derived the same way")
}

func TestMidOf_ReportsAnOverflow(t *testing.T) {
	big := OHLC{px("90000000000"), px("90000000000"), px("90000000000"), px("90000000000")}

	_, err := MidOf(big, big)

	require.Error(t, err)
}

func TestOHLC_Present(t *testing.T) {
	assert.False(t, OHLC{}.Present())
	assert.True(t, OHLC{Close: px("1.1")}.Present())
}

func TestBar_AvailabilityIsEstablishedOnlyByANonzeroAvailableAt(t *testing.T) {
	b := goodBar(t, utc(2024, 1, 2, 0))

	assert.True(t, b.IsComplete())
	assert.False(t, b.IsAvailable(b.AvailableAt.Add(-time.Nanosecond)))
	assert.True(t, b.IsAvailable(b.AvailableAt), "available at the instant itself")
	assert.True(t, b.IsAvailable(b.AvailableAt.Add(time.Hour)))

	b.AvailableAt = time.Time{}
	assert.False(t, b.IsComplete())
	assert.False(t, b.IsAvailable(utc(2030, 1, 1, 0)), "a zero AvailableAt is never available")
}

func TestBar_Validate(t *testing.T) {
	open := utc(2024, 1, 2, 0)
	for name, tc := range map[string]struct {
		mutate func(*Bar)
		want   error
	}{
		"valid":             {func(*Bar) {}, nil},
		"no time":           {func(b *Bar) { b.Time = time.Time{} }, ErrInvalidBar},
		"no availability":   {func(b *Bar) { b.AvailableAt = time.Time{} }, ErrInvalidBar},
		"available early":   {func(b *Bar) { b.AvailableAt = open.Add(-time.Second) }, ErrInvalidBar},
		"negative volume":   {func(b *Bar) { b.Volume = -1 }, ErrInvalidBar},
		"zero bid price":    {func(b *Bar) { b.Bid.Low = px("0") }, ErrInvalidBar},
		"zero ask price":    {func(b *Bar) { b.Ask.Open = px("0") }, ErrInvalidBar},
		"bid high low":      {func(b *Bar) { b.Bid.High = px("1.09000") }, ErrInvalidBar},
		"ask low high":      {func(b *Bar) { b.Ask.Low = px("1.20000") }, ErrInvalidBar},
		"crossed open":      {func(b *Bar) { b.Ask.Open = px("1.09990") }, ErrCrossed},
		"crossed close":     {func(b *Bar) { b.Ask.Close = px("1.10040") }, ErrCrossed},
		"crossed high":      {func(b *Bar) { b.Ask.High = px("1.10090") }, ErrCrossed},
		"crossed low":       {func(b *Bar) { b.Ask.Low = px("1.09890") }, ErrCrossed},
		"available at open": {func(b *Bar) { b.AvailableAt = b.Time }, nil},
		"mid only":          {func(b *Bar) { b.Bid, b.Ask = OHLC{}, OHLC{} }, nil},
		"bid only":          {func(b *Bar) { b.Ask, b.Mid = OHLC{}, OHLC{} }, nil},
		"ask only":          {func(b *Bar) { b.Bid, b.Mid = OHLC{}, OHLC{} }, nil},
		"bid and mid only":  {func(b *Bar) { b.Ask = OHLC{} }, nil},
		"no prices":         {func(b *Bar) { b.Bid, b.Ask, b.Mid = OHLC{}, OHLC{}, OHLC{} }, ErrInvalidBar},
		"mid absent":        {func(b *Bar) { b.Mid = OHLC{} }, ErrInvalidBar},
		"mid not midpoint":  {func(b *Bar) { b.Mid.High = px("1.20000") }, ErrInvalidBar},
		"mid partly zero":   {func(b *Bar) { b.Mid.Low = px("0") }, ErrInvalidBar},
		"mid shape":         {func(b *Bar) { b.Bid, b.Ask = OHLC{}, OHLC{}; b.Mid.High = px("1.00000") }, ErrInvalidBar},
		"mid overflow":      {func(b *Bar) { b.Bid.Open, b.Ask.Open = px("90000000000"), px("90000000000") }, ErrInvalidBar},
	} {
		t.Run(name, func(t *testing.T) {
			b := goodBar(t, open)
			tc.mutate(&b)

			err := b.Validate()

			if tc.want == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tc.want)
		})
	}
}
