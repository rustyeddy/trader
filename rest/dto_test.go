package rest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriceText_NeverRoundsAndKeepsEveryDigit(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"1.08726", "1.08726"},
		{"1.087265", "1.087265"},     // a mid price: six decimals survive
		{"1.08726512", "1.08726512"}, // the finest the price type carries
		{"1.1", "1.10000"},           // padded to the archive's width, same value
		{"150", "150.00000"},
	} {
		got := priceText(num.MustParsePrice(tc.in))
		assert.Equal(t, tc.want, got, tc.in)
		assert.True(t, num.MustParsePrice(got).Equal(num.MustParsePrice(tc.in)), "%s round-trips to the same price", tc.in)
	}
}

func TestBarOf_ASixDecimalMidSurvivesTheJSONUnchanged(t *testing.T) {
	bp, ap := num.MustParsePrice("1.08725"), num.MustParsePrice("1.08728")
	bid := marketdata.OHLC{Open: bp, High: bp, Low: bp, Close: bp}
	ask := marketdata.OHLC{Open: ap, High: ap, Low: ap, Close: ap}
	mid, err := marketdata.MidOf(bid, ask)
	require.NoError(t, err)
	require.Equal(t, "1.087265", mid.Open.String(), "the fixture's mid needs six decimals")

	b := marketdata.Bar{
		Time: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC), AvailableAt: time.Date(2024, 1, 2, 11, 0, 0, 0, time.UTC),
		Bid: bid, Ask: ask, Mid: mid,
	}
	raw, err := json.Marshal(barOf(b))
	require.NoError(t, err)
	var got struct {
		Mid struct{ Open, High, Low, Close string }
		Bid struct{ Open string }
	}
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "1.087265", got.Mid.Open)
	assert.Equal(t, "1.087265", got.Mid.Close)
	assert.Equal(t, "1.08725", got.Bid.Open)
}
