package data

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildRates_Defaults(t *testing.T) {
	rates := buildRates("")
	assert.Equal(t, 150.00, rates["USDJPY"])
	assert.Equal(t, 0.90, rates["USDCHF"])
	assert.Equal(t, 1.36, rates["USDCAD"])
}

func TestBuildRates_Override(t *testing.T) {
	rates := buildRates("USDJPY=152.50,USDCHF=0.88")
	assert.Equal(t, 152.50, rates["USDJPY"])
	assert.Equal(t, 0.88, rates["USDCHF"])
	assert.Equal(t, 1.36, rates["USDCAD"]) // default unchanged
}

func TestBuildRates_NormalisesInstrumentName(t *testing.T) {
	rates := buildRates("usdjpy=148.00")
	assert.Equal(t, 148.00, rates["USDJPY"])
}

func TestBuildRates_IgnoresBadEntries(t *testing.T) {
	rates := buildRates("USDJPY=notanumber,USDCHF=0.88")
	assert.Equal(t, 150.00, rates["USDJPY"]) // bad value → default preserved
	assert.Equal(t, 0.88, rates["USDCHF"])
}

func TestCommaInt(t *testing.T) {
	assert.Equal(t, "1,000", commaInt(1000))
	assert.Equal(t, "10,000", commaInt(10000))
	assert.Equal(t, "100,000", commaInt(100000))
	assert.Equal(t, "1,000,000", commaInt(1000000))
	assert.Equal(t, "999", commaInt(999))
}

func TestFmtUSD(t *testing.T) {
	assert.Equal(t, "$10,000", fmtUSD(10000))
	assert.Equal(t, "$100.00", fmtUSD(100))
	assert.Equal(t, "$6.6667", fmtUSD(6.6667))
}

func TestFmtUnits(t *testing.T) {
	assert.Equal(t, "100,000 (standard lot)", fmtUnits(100_000))
	assert.Equal(t, "10,000 (mini lot)", fmtUnits(10_000))
	assert.Equal(t, "1,000 (micro lot)", fmtUnits(1_000))
	assert.Equal(t, "5000", fmtUnits(5000))
}
