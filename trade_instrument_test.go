package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetInstrument_DirectAndMappedSymbols_Phase2 verifies expected behavior for this component.
func TestGetInstrument_DirectAndMappedSymbols_Phase2(t *testing.T) {
	t.Parallel()

	direct := GetInstrument("EURUSD")
	require.NotNil(t, direct)
	assert.Equal(t, "EURUSD", direct.Name)

	mapped := GetInstrument("EUR_USD")
	require.NotNil(t, mapped)
	assert.Equal(t, "EURUSD", mapped.Name)

	normalized := GetInstrument(" eur/usd ")
	require.NotNil(t, normalized)
	assert.Equal(t, "EURUSD", normalized.Name)
}

// TestGetInstrument_ReturnsCopy verifies expected behavior for this component.
func TestGetInstrument_ReturnsCopy(t *testing.T) {
	t.Parallel()

	first := GetInstrument("EURUSD")
	require.NotNil(t, first)
	first.Name = "MUTATED"
	first.MarginRate = 0

	second := GetInstrument("EURUSD")
	require.NotNil(t, second)
	assert.Equal(t, "EURUSD", second.Name)
	assert.Equal(t, Rate(20_000), second.MarginRate)
}

// TestGetInstrument_UnknownReturnsNil_Phase2 verifies expected behavior for this component.
func TestGetInstrument_UnknownReturnsNil_Phase2(t *testing.T) {
	t.Parallel()

	assert.Nil(t, GetInstrument("NOPE"))
}

// TestNormalizeInstrument_Phase2 verifies expected behavior for this component.
func TestNormalizeInstrument_Phase2(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "EURUSD", NormalizeInstrument(" eur/usd "))
	assert.Equal(t, "USDJPY", NormalizeInstrument("usd_jpy"))
}

// TestMajorInstruments_ReturnsCopy verifies expected behavior for this component.
func TestMajorInstruments_ReturnsCopy(t *testing.T) {
	t.Parallel()

	first := MajorInstruments()
	require.NotEmpty(t, first)
	first[0] = "MUTATED"

	second := MajorInstruments()
	require.NotEmpty(t, second)
	assert.Equal(t, "EURUSD", second[0])
}

// TestApproximateUSDPerUnit verifies expected behavior for this component.
func TestApproximateUSDPerUnit(t *testing.T) {
	t.Parallel()

	rate, ok := ApproximateUSDPerUnit("jpy")
	require.True(t, ok)
	assert.InDelta(t, 0.0067, rate, 1e-9)

	_, ok = ApproximateUSDPerUnit("NOPE")
	assert.False(t, ok)
}

// TestInstrumentPriceAndPipMath_Phase2 verifies expected behavior for this component.
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

// TestInstrumentPipSize_Phase2 verifies expected behavior for this component.
func TestInstrumentPipSize_Phase2(t *testing.T) {
	t.Parallel()

	eurusd := GetInstrument("EURUSD")
	require.NotNil(t, eurusd)
	assert.InDelta(t, 0.0001, eurusd.PipSize(), 1e-12)

	usdjpy := GetInstrument("USDJPY")
	require.NotNil(t, usdjpy)
	assert.InDelta(t, 0.01, usdjpy.PipSize(), 1e-12)
}
