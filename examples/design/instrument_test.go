package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetInstrument(t *testing.T) {
	inst, ok := getInstrument("EURUSD")
	require.True(t, ok)
	assert.Equal(t, "EURUSD", inst.symbol)
	assert.Equal(t, "EUR", inst.base)
	assert.Equal(t, "USD", inst.quote)
	assert.Positive(t, inst.minUnits)
	assert.Positive(t, inst.unitStep)

	inst, ok = getInstrument("NOPE")
	assert.False(t, ok)
	assert.Nil(t, inst)
}

func TestInstruments_KeyMatchesSymbol(t *testing.T) {
	for key, inst := range instruments {
		assert.Equal(t, key, inst.symbol)
	}
}

func TestRate_Constants(t *testing.T) {
	assert.Equal(t, int64(1_000_000), rateScale)
	assert.Equal(t, rateScale/100, int64(rateOnePercent), "1% of the scale")
	assert.Equal(t, rateScale/50, int64(rateTwoPercent), "2% of the scale")
	assert.Equal(t, 2*rateOnePercent, rateTwoPercent)
}
