package instrument

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCurrencyPairIDIsIndependentOfProviderSpelling is the acceptance-
// criterion test: three different "provider adapters" each parse a
// different spelling of the same pair — "EUR_USD", "EURUSD", "EUR/USD" —
// into base/quote currencies before calling CurrencyPairID. The instrument
// package never sees the original spelling, so all three must resolve to
// the identical ID.
func TestCurrencyPairIDIsIndependentOfProviderSpelling(t *testing.T) {
	parseOandaStyle := func(s string) (num.Currency, num.Currency) {
		// "EUR_USD"
		return num.MustParseCurrency(s[:3]), num.MustParseCurrency(s[4:])
	}
	parseCompactStyle := func(s string) (num.Currency, num.Currency) {
		// "EURUSD"
		return num.MustParseCurrency(s[:3]), num.MustParseCurrency(s[3:])
	}
	parseSlashStyle := func(s string) (num.Currency, num.Currency) {
		// "EUR/USD"
		return num.MustParseCurrency(s[:3]), num.MustParseCurrency(s[4:])
	}

	base1, quote1 := parseOandaStyle("EUR_USD")
	base2, quote2 := parseCompactStyle("EURUSD")
	base3, quote3 := parseSlashStyle("EUR/USD")

	id1 := CurrencyPairID(base1, quote1)
	id2 := CurrencyPairID(base2, quote2)
	id3 := CurrencyPairID(base3, quote3)

	assert.True(t, id1.Equal(id2))
	assert.True(t, id2.Equal(id3))
	assert.Equal(t, "fx:EUR/USD", id1.String())
}

func TestCurrencyPairIDDiffersByDirection(t *testing.T) {
	eur := num.MustParseCurrency("EUR")
	usd := num.MustParseCurrency("USD")

	eurUsd := CurrencyPairID(eur, usd)
	usdEur := CurrencyPairID(usd, eur)

	assert.False(t, eurUsd.Equal(usdEur))
}

func TestEquityAndETFIDsNeverCollide(t *testing.T) {
	eq := EquityID("NASDAQ", "SPY")
	etf := ETFID("NASDAQ", "SPY")

	assert.False(t, eq.Equal(etf))
}

func TestEquityIDNormalizesCase(t *testing.T) {
	upper := EquityID("NASDAQ", "AAPL")
	lower := EquityID("nasdaq", "aapl")
	padded := EquityID("  NASDAQ  ", "  AAPL  ")

	assert.True(t, upper.Equal(lower))
	assert.True(t, upper.Equal(padded))
	assert.Equal(t, "eq:NASDAQ:AAPL", upper.String())
}

func TestFutureIDDiffersByExpiration(t *testing.T) {
	dec := FutureID("ES", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))
	mar := FutureID("ES", time.Date(2027, time.March, 20, 0, 0, 0, 0, time.UTC))

	assert.False(t, dec.Equal(mar))
	assert.Equal(t, "fut:ES:2026-12", dec.String())
	assert.Equal(t, "fut:ES:2027-03", mar.String())
}

func TestFutureIDIsInsensitiveToDayWithinTheSameMonth(t *testing.T) {
	early := FutureID("ES", time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC))
	late := FutureID("ES", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))

	assert.True(t, early.Equal(late), "FutureID uses month granularity by design")
}

func TestContinuousSeriesIDNeverCollidesWithAFutureOnTheSameRoot(t *testing.T) {
	future := FutureID("ES", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))
	continuous := ContinuousSeriesID("ES")

	assert.False(t, future.Equal(continuous))
	assert.Equal(t, "cont:ES", continuous.String())
}

func TestIndexID(t *testing.T) {
	assert.Equal(t, "idx:SPX", IndexID("SPX").String())
	assert.Equal(t, "idx:SPX", IndexID(" spx ").String())
}

func TestIDConstructorsReturnZeroForInvalidInput(t *testing.T) {
	eur := num.MustParseCurrency("EUR")
	usd := num.MustParseCurrency("USD")
	dec := time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC)

	assert.True(t, CurrencyPairID(num.Currency{}, usd).IsZero())
	assert.True(t, CurrencyPairID(eur, num.Currency{}).IsZero())
	assert.True(t, CurrencyPairID(eur, eur).IsZero(), "a pair cannot have equal base and quote")

	assert.True(t, EquityID("", "AAPL").IsZero())
	assert.True(t, EquityID("NASDAQ", "").IsZero())

	assert.True(t, ETFID("", "SPY").IsZero())
	assert.True(t, ETFID("NASDAQ", "").IsZero())

	assert.True(t, FutureID("", dec).IsZero())
	assert.True(t, FutureID("ES", time.Time{}).IsZero())

	assert.True(t, ContinuousSeriesID("").IsZero())
	assert.True(t, IndexID("").IsZero())
}

// TestEquityIDRejectsAmbiguousSeparatorCharacters is the collision-
// prevention test: without character-set validation,
// EquityID("A:B", "C") and EquityID("A", "B:C") would both collapse to
// "eq:A:B:C". Rejecting ':' at the input boundary (returning the zero ID)
// closes that collision instead of trying to escape it.
func TestEquityIDRejectsAmbiguousSeparatorCharacters(t *testing.T) {
	a := EquityID("A:B", "C")
	b := EquityID("A", "B:C")

	assert.True(t, a.IsZero())
	assert.True(t, b.IsZero())
}

func TestIDZeroValue(t *testing.T) {
	var id ID
	assert.True(t, id.IsZero())
	assert.Equal(t, "", id.String())

	nonZero := IndexID("SPX")
	assert.False(t, nonZero.IsZero())
	require.False(t, id.Equal(nonZero))
}
