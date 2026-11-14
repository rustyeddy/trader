package marketdata

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func px(s string) num.Price { return num.MustParsePrice(s) }

func utc(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

// goodBar is a valid bar with bid, ask and the mid derived from them.
func goodBar(t *testing.T) Bar {
	t.Helper()
	open := utc(2024, 1, 2, 0)
	bid := OHLC{px("1.10000"), px("1.10100"), px("1.09900"), px("1.10050")}
	ask := OHLC{px("1.10010"), px("1.10111"), px("1.09911"), px("1.10061")}
	mid, err := MidOf(bid, ask)
	require.NoError(t, err)
	return Bar{Time: open, AvailableAt: open.Add(time.Hour), Volume: 100, Bid: bid, Ask: ask, Mid: mid}
}

func series(o, h, l, c string) OHLC { return OHLC{px(o), px(h), px(l), px(c)} }

func TestMidOf_IsTheExactMidpointOfBidAndAsk(t *testing.T) {
	b := goodBar(t)

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

func TestOHLC_IsZero(t *testing.T) {
	assert.True(t, OHLC{}.IsZero())
	assert.False(t, OHLC{Close: px("1.1")}.IsZero(), "one price is enough to be present, if not valid")
}

func TestBar_OHLC_ChoosesMidThenTradeThenBidThenAsk(t *testing.T) {
	mid, trade := series("1.2", "1.3", "1.1", "1.25"), series("1.21", "1.31", "1.11", "1.26")
	bid, ask := series("1.0", "1.1", "0.9", "1.05"), series("1.01", "1.11", "0.91", "1.06")
	for name, tc := range map[string]struct {
		bar  Bar
		want OHLC
	}{
		"everything":      {Bar{Mid: mid, Trade: trade, Bid: bid, Ask: ask}, mid},
		"no mid":          {Bar{Trade: trade, Bid: bid, Ask: ask}, trade},
		"no mid or trade": {Bar{Bid: bid, Ask: ask}, bid},
		"ask only":        {Bar{Ask: ask}, ask},
		"mid only":        {Bar{Mid: mid}, mid},
		"trade only":      {Bar{Trade: trade}, trade},
		"nothing":         {Bar{}, OHLC{}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.bar.OHLC())
		})
	}
}

func TestBar_OHLC_NeverSubstitutesOneSideForTheOtherInTheFields(t *testing.T) {
	b := Bar{Bid: series("1.0", "1.1", "0.9", "1.05")}

	assert.True(t, b.Ask.IsZero(), "the ask field stays absent; only OHLC() falls back")
	assert.Equal(t, b.Bid, b.OHLC())
}

func TestBar_AvailabilityIsEstablishedOnlyByANonzeroAvailableAt(t *testing.T) {
	b := goodBar(t)

	assert.True(t, b.IsComplete())
	assert.False(t, b.IsAvailable(b.AvailableAt.Add(-time.Nanosecond)))
	assert.True(t, b.IsAvailable(b.AvailableAt), "available at the instant itself")
	assert.True(t, b.IsAvailable(b.AvailableAt.Add(time.Hour)))

	b.AvailableAt = time.Time{}
	assert.False(t, b.IsComplete())
	assert.False(t, b.IsAvailable(utc(2030, 1, 1, 0)), "a zero AvailableAt is never available")
}

func TestBar_Validate(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*Bar)
		want   error
	}{
		"valid":             {func(*Bar) {}, nil},
		"no time":           {func(b *Bar) { b.Time = time.Time{} }, ErrInvalidBar},
		"no availability":   {func(b *Bar) { b.AvailableAt = time.Time{} }, ErrInvalidBar},
		"available early":   {func(b *Bar) { b.AvailableAt = b.Time.Add(-time.Second) }, ErrInvalidBar},
		"available at open": {func(b *Bar) { b.AvailableAt = b.Time }, nil},
		"negative volume":   {func(b *Bar) { b.Volume = -1 }, ErrInvalidBar},
		"zero volume":       {func(b *Bar) { b.Volume = 0 }, nil},
		"zero bid price":    {func(b *Bar) { b.Bid.Low = px("0") }, ErrInvalidBar},
		"zero ask price":    {func(b *Bar) { b.Ask.Open = px("0") }, ErrInvalidBar},
		"bid high low":      {func(b *Bar) { b.Bid.High = px("1.09000") }, ErrInvalidBar},
		"ask low high":      {func(b *Bar) { b.Ask.Low = px("1.20000") }, ErrInvalidBar},
		"crossed open":      {func(b *Bar) { b.Ask.Open = px("1.09990") }, ErrCrossed},
		"crossed close":     {func(b *Bar) { b.Ask.Close = px("1.10040") }, ErrCrossed},
		"crossed high":      {func(b *Bar) { b.Ask.High = px("1.10090") }, ErrCrossed},
		"crossed low":       {func(b *Bar) { b.Ask.Low = px("1.09890") }, ErrCrossed},
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
		"trade only": {func(b *Bar) {
			*b = Bar{Time: b.Time, AvailableAt: b.AvailableAt, Trade: series("1.1", "1.2", "1.0", "1.15")}
		}, nil},
		"trade with quotes": {func(b *Bar) { b.Trade = series("1.1", "1.2", "1.0", "1.15") }, nil},
		"trade partly zero": {func(b *Bar) { b.Trade = OHLC{Open: px("1.1"), High: px("1.2"), Low: px("1.0")} }, ErrInvalidBar},
		"trade shape":       {func(b *Bar) { b.Trade = series("1.1", "1.0", "1.0", "1.15") }, ErrInvalidBar},
		// A trade price is unrelated to the quotes, so it can sit outside them.
		"trade outside the quotes": {func(b *Bar) { b.Trade = series("2.1", "2.2", "2.0", "2.15") }, nil},
	} {
		t.Run(name, func(t *testing.T) {
			b := goodBar(t)
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
