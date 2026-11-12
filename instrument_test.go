package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstruments_EURUSD(t *testing.T) {
	inst, ok := instruments["EURUSD"]
	require.True(t, ok)
	assert.Equal(t, "EURUSD", inst.symbol)
	assert.False(t, inst.minUnits.IsZero())
	assert.False(t, inst.unitStep.IsZero())
}

func TestInstruments_KeyMatchesSymbol(t *testing.T) {
	for key, inst := range instruments {
		assert.Equal(t, key, inst.symbol)
	}
}

func TestRate_Constants(t *testing.T) {
	assert.Equal(t, rt(10_000), rateOnePercent, "1%")
	assert.Equal(t, rt(20_000), rateTwoPercent, "2%")
	assert.Equal(t, rt(50_000), rateFivePercent, "5%")
	assert.Equal(t, rt(1_000_000), rateOne, "100%")
	assert.Equal(t, rt(500_000), rateHalf, "50%")
}
