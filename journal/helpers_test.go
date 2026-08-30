package journal_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

var testIDs = id.NewGenerator(clock.NewSimulated(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(1, 2))

func mustRunID(t *testing.T) id.RunID {
	t.Helper()
	v, err := id.GenerateRunID(testIDs)
	require.NoError(t, err)
	return v
}

func mustEventID(t *testing.T) id.EventID {
	t.Helper()
	v, err := id.GenerateEventID(testIDs)
	require.NoError(t, err)
	return v
}

func mustIntentID(t *testing.T) id.IntentID {
	t.Helper()
	v, err := id.GenerateIntentID(testIDs)
	require.NoError(t, err)
	return v
}

func mustCorrelationID(t *testing.T) id.CorrelationID {
	t.Helper()
	v, err := id.GenerateCorrelationID(testIDs)
	require.NoError(t, err)
	return v
}

func mustEurUsdListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}
