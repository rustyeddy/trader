package backtest

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
