package instrument_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIDJSONRoundTrip(t *testing.T) {
	id := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	data, err := json.Marshal(id)
	require.NoError(t, err)
	assert.Equal(t, `"fx:EUR/USD"`, string(data))

	var got instrument.ID
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, id.Equal(got))
}

func TestIDJSONZeroValueRoundTrip(t *testing.T) {
	var id instrument.ID
	data, err := json.Marshal(id)
	require.NoError(t, err)
	assert.Equal(t, `""`, string(data))

	var got instrument.ID
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, got.IsZero())
}

func TestIDUnmarshalJSONRejectsNonString(t *testing.T) {
	var id instrument.ID
	err := json.Unmarshal([]byte(`123`), &id)
	require.ErrorIs(t, err, instrument.ErrInvalidID)
}

func TestIDMarshalJSONDeterministic(t *testing.T) {
	id := instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	a, err := json.Marshal(id)
	require.NoError(t, err)
	b, err := json.Marshal(id)
	require.NoError(t, err)
	assert.Equal(t, a, b)
}

func TestIDUnmarshalJSONRejectsUnrecognizedKindPrefix(t *testing.T) {
	var id instrument.ID
	err := json.Unmarshal([]byte(`"bogus:whatever"`), &id)
	require.ErrorIs(t, err, instrument.ErrInvalidID)
}

func TestIDUnmarshalJSONAcceptsEveryConstructorPrefix(t *testing.T) {
	for _, s := range []string{
		"fx:EUR/USD",
		"eq:NASDAQ:AAPL",
		"etf:NYSE:SPY",
		"fut:ES:2026-12",
		"cont:ES",
		"idx:SPX",
	} {
		var id instrument.ID
		data, err := json.Marshal(s)
		require.NoError(t, err, s)
		require.NoError(t, json.Unmarshal(data, &id), s)
		assert.Equal(t, s, id.String(), s)
	}
}

func TestIDUnmarshalJSONRejectsMalformedBodies(t *testing.T) {
	for _, s := range []string{
		"fx:",               // no pair
		"fx:EUR",            // no quote
		"fx:EUR/",           // empty quote
		"fx:/USD",           // empty base
		"fx:eur/usd",        // not canonical
		"fx:EUR/EUR",        // base equals quote
		"fx:EUR/USD/GBP",    // extra segment
		"fx:EU/USD",         // not a currency code
		"eq:NASDAQ",         // no ticker
		"eq:NASDAQ:",        // empty ticker
		"eq::AAPL",          // empty exchange
		"eq:nasdaq:aapl",    // not canonical
		"eq: NASDAQ:AAPL",   // surrounding space
		"eq:NASDAQ:AAPL:X",  // extra segment
		"eq:NAS/DAQ:AAPL",   // separator character in a part
		"etf:NYSE:S PY",     // space in a part
		"fut:ES",            // no contract month
		"fut:ES:2026-13",    // no such month
		"fut:ES:2026-1",     // month not two digits
		"fut:ES:2026-12-01", // a date, not a month
		"fut:es:2026-12",    // not canonical
		"cont:",             // empty root
		"cont:ES:2026-12",   // a contract, not a series
		"cont:a b",          // space
		"idx:",              // empty name
		"idx:s&p",           // outside the alphabet
		"EQ:NASDAQ:AAPL",    // prefix is case sensitive
		"NASDAQ:AAPL",       // no kind
		"fx",                // no separator
		"bogus:whatever",    // unknown kind
		"fut:ES:x2026-12",   // a stray character before the year
	} {
		var id instrument.ID
		data, err := json.Marshal(s)
		require.NoError(t, err)
		err = json.Unmarshal(data, &id)
		require.ErrorIs(t, err, instrument.ErrInvalidID, "%q", s)
		assert.True(t, id.IsZero(), "a refused input leaves the zero ID: %q", s)
	}
}

func TestIDJSONRoundTripsEveryConstructor(t *testing.T) {
	expiry := time.Date(2026, time.December, 18, 0, 0, 0, 0, time.UTC)
	for name, id := range map[string]instrument.ID{
		"pair":        instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD")),
		"equity":      instrument.EquityID("nasdaq", " aapl "),
		"dotted":      instrument.EquityID("NYSE", "BRK.B"),
		"dashed":      instrument.ETFID("NYSE", "RDS-A"),
		"future":      instrument.FutureID("ES", expiry),
		"digit root":  instrument.FutureID("6E", expiry),
		"continuous":  instrument.ContinuousSeriesID("ES"),
		"index":       instrument.IndexID("SPX"),
		"zero":        {},
		"december 31": instrument.FutureID("CL", time.Date(2026, time.December, 31, 23, 0, 0, 0, time.UTC)),
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(id)
			require.NoError(t, err)
			var got instrument.ID
			require.NoError(t, json.Unmarshal(data, &got))
			assert.True(t, id.Equal(got), "%q", id)
		})
	}
}

func TestIDUnmarshalJSONReplacesAPreviousValue(t *testing.T) {
	id := instrument.IndexID("SPX")

	require.NoError(t, json.Unmarshal([]byte(`""`), &id))

	assert.True(t, id.IsZero(), "an empty string decodes to the zero ID, even over a value")
}
