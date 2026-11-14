package canonical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMid_IsTheExactMidpointOfBidAndAsk(t *testing.T) {
	b := goodBar(t, utc(2024, 1, 2, 0))

	m, err := b.Mid()

	require.NoError(t, err)
	assert.Equal(t, "1.100050", pad(m.Open.String()), "half a pip is exact")
	assert.Equal(t, px("1.101055"), m.High)
	assert.Equal(t, px("1.099055"), m.Low)
	assert.Equal(t, px("1.100555"), m.Close)
}

// pad right-pads the decimals so the assertion reads like the file's text.
func pad(s string) string {
	for len(s) < 8 {
		s += "0"
	}
	return s
}

func TestMid_ReportsAnOverflow(t *testing.T) {
	b := goodBar(t, utc(2024, 1, 2, 0))
	b.Bid.Open = px("90000000000")
	b.Ask.Open = px("90000000000")

	_, err := b.Mid()

	require.Error(t, err)
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
