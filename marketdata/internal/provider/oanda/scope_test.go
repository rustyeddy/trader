package oanda

import (
	"testing"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSymbolInScopePairs(t *testing.T) {
	// A representative spread of the 24 in-scope pairs, including a JPY pair
	// and a cross with no USD leg.
	for _, sym := range []string{"EURUSD", "USDJPY", "GBPJPY", "EURGBP", "AUDNZD"} {
		id, err := resolveSymbol(sym)
		require.NoError(t, err, sym)
		want := instrument.CurrencyPairID(
			num.MustParseCurrency(sym[:3]), num.MustParseCurrency(sym[3:]))
		assert.Equal(t, want, id, sym)
		assert.False(t, id.IsZero(), sym)
	}
}

func TestResolveSymbolOutOfScope(t *testing.T) {
	// Out of scope whether the non-FX leg is the base (XAUUSD) or the quote
	// (USDXAU); both must be recognized, not treated as FX.
	for _, sym := range []string{"XAUUSD", "USDXAU"} {
		id, err := resolveSymbol(sym)
		assert.True(t, id.IsZero(), sym)
		assert.ErrorIs(t, err, ErrInstrumentOutOfScope, sym)
	}
}

func TestResolveSymbolMalformed(t *testing.T) {
	for _, sym := range []string{"EUR", "EURUSDX", "eurusd", "EUR123"} {
		_, err := resolveSymbol(sym)
		assert.ErrorIs(t, err, ErrMalformedData, sym)
	}
}

func TestResolveInterval(t *testing.T) {
	cases := map[string]marketdata.Interval{
		"m1": marketdata.M1,
		"h1": marketdata.H1,
		"h4": marketdata.H4,
		"d1": marketdata.D1,
		"d":  marketdata.D1,
	}
	for token, want := range cases {
		got, err := resolveInterval(token)
		require.NoError(t, err, token)
		assert.Equal(t, want, got, token)
	}
}

func TestResolveIntervalUnsupported(t *testing.T) {
	for _, token := range []string{"w1", "w", "m5", "", "H1"} {
		_, err := resolveInterval(token)
		assert.ErrorIs(t, err, ErrUnsupportedInterval, token)
	}
}
