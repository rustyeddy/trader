package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetInstrument_DirectAndMappedSymbols_Phase2(t *testing.T) {
	t.Parallel()

	direct := GetInstrument("EURUSD")
	require.NotNil(t, direct)
	assert.Equal(t, "EURUSD", direct.Name)

	mapped := GetInstrument("EUR_USD")
	require.NotNil(t, mapped)
	assert.Equal(t, "EURUSD", mapped.Name)
}

func TestGetInstrument_UnknownReturnsNil_Phase2(t *testing.T) {
	t.Parallel()

	assert.Nil(t, GetInstrument("NOPE"))
}

func TestNormalizeInstrument_Phase2(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "EURUSD", NormalizeInstrument(" eur/usd "))
	assert.Equal(t, "USDJPY", NormalizeInstrument("usd_jpy"))
}

func TestInstrumentPriceAndPipMath_Phase2(t *testing.T) {
	t.Parallel()

	eurusd := GetInstrument("EURUSD")
	require.NotNil(t, eurusd)
	assert.Equal(t, Price(10), eurusd.PriceUnitsPerPip())
	assert.Equal(t, Price(25), eurusd.PriceDeltaFromPips(Pips(25)))

	px := PriceFromFloat(1.10000)
	assert.Equal(t, px+Price(25), eurusd.AddPips(px, Pips(25)))
	assert.Equal(t, px-Price(25), eurusd.SubPips(px, Pips(25)))
}

func TestInstrumentPipSize_Phase2(t *testing.T) {
	t.Parallel()

	eurusd := GetInstrument("EURUSD")
	require.NotNil(t, eurusd)
	assert.InDelta(t, 0.0001, eurusd.PipSize(), 1e-12)

	usdjpy := GetInstrument("USDJPY")
	require.NotNil(t, usdjpy)
	assert.InDelta(t, 0.01, usdjpy.PipSize(), 1e-12)
}
