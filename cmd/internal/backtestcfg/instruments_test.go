package backtestcfg

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
)

func TestResolveInstrumentSetRegistersStooqETF(t *testing.T) {
	marketResolver := instrument.NewMemoryResolver()
	simResolver := instrument.NewMemoryResolver()

	set, err := resolveInstrumentSet([]string{"SPY"}, "stooq", marketResolver, simResolver)
	require.NoError(t, err)

	wantID := instrument.ETFID("ARCA", "SPY")
	require.Len(t, set.ids, 1)
	require.True(t, set.ids[0].Equal(wantID))

	marketListing, err := marketResolver.ResolveInstrument(wantID, "stooq", "")
	require.NoError(t, err)
	require.Equal(t, "SPY", marketListing.Symbol())

	simListing, err := simResolver.ResolveInstrument(wantID, "sim", "")
	require.NoError(t, err)
	require.Equal(t, "SPY", simListing.Symbol())
}

func TestResolveInstrumentSetRegistersFXUnderDataAndSim(t *testing.T) {
	marketResolver := instrument.NewMemoryResolver()
	simResolver := instrument.NewMemoryResolver()

	set, err := resolveInstrumentSet([]string{"usdjpy", "EURUSD"}, "oanda", marketResolver, simResolver)
	require.NoError(t, err)
	require.Len(t, set.ids, 2)
	for _, id := range set.ids {
		_, err := marketResolver.ResolveInstrument(id, "oanda", "")
		require.NoError(t, err)
		_, err = simResolver.ResolveInstrument(id, "sim", "")
		require.NoError(t, err)
	}
}

func TestResolveInstrumentSetRejectsEquityWithoutReference(t *testing.T) {
	_, err := resolveInstrumentSet([]string{"MSFT"}, "stooq", instrument.NewMemoryResolver(), instrument.NewMemoryResolver())
	require.EqualError(t, err, `unsupported equity "MSFT" for provider "stooq": add reference metadata before backtesting it`)
}

func TestResolveInstrumentSetRejectsDuplicateSymbol(t *testing.T) {
	_, err := resolveInstrumentSet([]string{"SPY", "spy"}, "stooq", instrument.NewMemoryResolver(), instrument.NewMemoryResolver())
	require.ErrorContains(t, err, `duplicate --symbol "SPY"`)
}
