package instrument

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustFuture(t *testing.T) Instrument {
	t.Helper()
	inst, err := NewFuture("ES", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	return inst
}

func TestNewCurrencyPair(t *testing.T) {
	eur := num.MustParseCurrency("EUR")
	usd := num.MustParseCurrency("USD")

	inst, err := NewCurrencyPair(eur, usd)
	require.NoError(t, err)

	assert.Equal(t, KindCurrencyPair, inst.Kind())
	assert.Equal(t, "fx:EUR/USD", inst.ID().String())

	base, ok := inst.Base()
	assert.True(t, ok)
	assert.True(t, base.Equal(eur))

	quote, ok := inst.Quote()
	assert.True(t, ok)
	assert.True(t, quote.Equal(usd))

	_, ok = inst.Exchange()
	assert.False(t, ok)
}

func TestNewCurrencyPairRejectsInvalidOrMatchingCurrencies(t *testing.T) {
	eur := num.MustParseCurrency("EUR")

	_, err := NewCurrencyPair(eur, eur)
	require.ErrorIs(t, err, ErrInvalidInstrument)

	_, err = NewCurrencyPair(num.Currency{}, eur)
	require.ErrorIs(t, err, ErrInvalidInstrument)

	_, err = NewCurrencyPair(eur, num.Currency{})
	require.ErrorIs(t, err, ErrInvalidInstrument)
}

func TestNewEquity(t *testing.T) {
	inst, err := NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)

	assert.Equal(t, KindEquity, inst.Kind())
	assert.Equal(t, "eq:NASDAQ:AAPL", inst.ID().String())

	exchange, ok := inst.Exchange()
	assert.True(t, ok)
	assert.Equal(t, "NASDAQ", exchange)

	ticker, ok := inst.Ticker()
	assert.True(t, ok)
	assert.Equal(t, "AAPL", ticker)

	_, ok = inst.Base()
	assert.False(t, ok)
}

func TestNewEquityRejectsEmptyExchangeOrTicker(t *testing.T) {
	_, err := NewEquity("", "AAPL")
	require.ErrorIs(t, err, ErrInvalidInstrument)

	_, err = NewEquity("NASDAQ", "")
	require.ErrorIs(t, err, ErrInvalidInstrument)

	_, err = NewEquity("   ", "AAPL")
	require.ErrorIs(t, err, ErrInvalidInstrument)
}

func TestNewETF(t *testing.T) {
	inst, err := NewETF("NASDAQ", "SPY")
	require.NoError(t, err)

	assert.Equal(t, KindETF, inst.Kind())
	assert.Equal(t, "etf:NASDAQ:SPY", inst.ID().String())

	exchange, ok := inst.Exchange()
	assert.True(t, ok)
	assert.Equal(t, "NASDAQ", exchange)
}

func TestNewETFRejectsEmptyExchangeOrTicker(t *testing.T) {
	_, err := NewETF("", "SPY")
	require.ErrorIs(t, err, ErrInvalidInstrument)

	_, err = NewETF("NASDAQ", "")
	require.ErrorIs(t, err, ErrInvalidInstrument)
}

func TestNewFuture(t *testing.T) {
	expiration := time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC)
	inst, err := NewFuture("ES", expiration)
	require.NoError(t, err)

	assert.Equal(t, KindFuture, inst.Kind())
	assert.Equal(t, "fut:ES:2026-12", inst.ID().String())

	root, ok := inst.Root()
	assert.True(t, ok)
	assert.Equal(t, "ES", root)

	want := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)
	got, ok := inst.Expiration()
	assert.True(t, ok)
	assert.True(t, got.Equal(want), "Expiration returns the canonical first-of-month value, not the exact input")

	_, ok = inst.Name()
	assert.False(t, ok)
}

func TestNewFutureRejectsInvalidRootOrZeroExpiration(t *testing.T) {
	_, err := NewFuture("", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))
	require.ErrorIs(t, err, ErrInvalidInstrument)

	_, err = NewFuture("ES", time.Time{})
	require.ErrorIs(t, err, ErrInvalidInstrument)

	_, err = NewFuture("E:S", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))
	require.ErrorIs(t, err, ErrInvalidInstrument)
}

func TestTwoFutureContractsOnTheSameRootAreDistinctInstruments(t *testing.T) {
	dec, err := NewFuture("ES", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	mar, err := NewFuture("ES", time.Date(2027, time.March, 20, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	assert.False(t, dec.ID().Equal(mar.ID()))
}

// TestTwoExpirationsInTheSameMonthProduceTheSameInstrument is the
// identity-invariant test: since FutureID only encodes year and month,
// Instrument.Expiration must be normalized to match exactly, or two
// Instrument values could share an ID while disagreeing about
// Expiration.
func TestTwoExpirationsInTheSameMonthProduceTheSameInstrument(t *testing.T) {
	early, err := NewFuture("ES", time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	late, err := NewFuture("ES", time.Date(2026, time.December, 19, 15, 30, 0, 0, time.UTC))
	require.NoError(t, err)

	assert.True(t, early.ID().Equal(late.ID()))

	earlyExp, _ := early.Expiration()
	lateExp, _ := late.Expiration()
	assert.True(t, earlyExp.Equal(lateExp), "Expiration is normalized to contract month, matching ID exactly")
}

func TestNewContinuousSeries(t *testing.T) {
	inst, err := NewContinuousSeries("ES")
	require.NoError(t, err)

	assert.Equal(t, KindContinuousSeries, inst.Kind())
	assert.Equal(t, "cont:ES", inst.ID().String())

	root, ok := inst.Root()
	assert.True(t, ok)
	assert.Equal(t, "ES", root)

	_, ok = inst.Expiration()
	assert.False(t, ok, "a continuous series has no single expiration")
}

func TestNewContinuousSeriesRejectsEmptyOrInvalidRoot(t *testing.T) {
	_, err := NewContinuousSeries("")
	require.ErrorIs(t, err, ErrInvalidInstrument)

	_, err = NewContinuousSeries("E:S")
	require.ErrorIs(t, err, ErrInvalidInstrument)
}

func TestContinuousSeriesIsDistinctFromAnyFutureOnTheSameRoot(t *testing.T) {
	future, err := NewFuture("ES", time.Date(2026, time.December, 19, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	continuous, err := NewContinuousSeries("ES")
	require.NoError(t, err)

	assert.False(t, future.ID().Equal(continuous.ID()))
	assert.NotEqual(t, future.Kind(), continuous.Kind())
}

func TestNewIndex(t *testing.T) {
	inst, err := NewIndex("SPX")
	require.NoError(t, err)

	assert.Equal(t, KindIndex, inst.Kind())
	assert.Equal(t, "idx:SPX", inst.ID().String())

	name, ok := inst.Name()
	assert.True(t, ok)
	assert.Equal(t, "SPX", name)
}

func TestNewIndexRejectsEmptyOrInvalidName(t *testing.T) {
	_, err := NewIndex("   ")
	require.ErrorIs(t, err, ErrInvalidInstrument)

	_, err = NewIndex("SPX:500")
	require.ErrorIs(t, err, ErrInvalidInstrument)
}

func TestNewEquityRejectsInvalidCharacters(t *testing.T) {
	_, err := NewEquity("A:B", "C")
	require.ErrorIs(t, err, ErrInvalidInstrument)

	_, err = NewEquity("A", "B/C")
	require.ErrorIs(t, err, ErrInvalidInstrument)
}

func TestInstrumentZeroValue(t *testing.T) {
	var inst Instrument
	assert.True(t, inst.ID().IsZero())
	assert.False(t, inst.Kind().IsValid())
}
