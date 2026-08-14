package oanda

import (
	"testing"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// the24Pairs is the audited in-scope corpus, listed independently of the
// package's own inScopeFXPairs so the test would catch an accidental edit to
// that set.
var the24Pairs = []string{
	"AUDCAD", "AUDCHF", "AUDJPY", "AUDNZD", "AUDUSD",
	"CADJPY", "CHFJPY",
	"EURAUD", "EURCAD", "EURCHF", "EURGBP", "EURJPY", "EURNZD", "EURUSD",
	"GBPAUD", "GBPCAD", "GBPJPY", "GBPNZD", "GBPUSD",
	"NZDJPY", "NZDUSD",
	"USDCAD", "USDCHF", "USDJPY",
}

func TestResolveSymbolAll24InScopePairs(t *testing.T) {
	require.Len(t, the24Pairs, 24)
	for _, sym := range the24Pairs {
		id, err := resolveSymbol(sym)
		require.NoError(t, err, sym)
		want := instrument.CurrencyPairID(
			num.MustParseCurrency(sym[:3]), num.MustParseCurrency(sym[3:]))
		assert.Equal(t, want, id, sym)
		assert.False(t, id.IsZero(), sym)
	}
}

func TestResolveSymbolOutOfScope(t *testing.T) {
	// Out of scope for three distinct reasons, all of which must resolve to
	// ErrInstrumentOutOfScope rather than being treated as an FX pair:
	//   - XAUUSD / USDXAU: gold is not an FX leg at all.
	//   - USDEUR: a well-formed pair in the same currency universe, but not a
	//     member of the preserved 24-pair corpus (only EURUSD is).
	for _, sym := range []string{"XAUUSD", "USDXAU", "USDEUR", "CADNZD"} {
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
	cases := map[string]RawInterval{
		"m1": RawM1,
		"h1": RawH1,
		"h4": RawH4,
		"d1": RawD1,
		"d":  RawD1,
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
