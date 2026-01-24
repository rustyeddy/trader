package risk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPipSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		loc  int
		want float64
	}{
		{"zero", 0, 1},
		{"negative2", -2, 0.01},
		{"positive1", 1, 10},
		{"negative4", -4, 0.0001},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := pipSize(tt.loc)
			assert.InDelta(t, tt.want, got, 1e-12)
		})
	}
}

func TestCalculate_SimpleUSDQuote(t *testing.T) {
	t.Parallel()

	in := Inputs{
		Equity:         10000,
		RiskPct:        0.01,
		EntryPrice:     1.2000,
		StopPrice:      1.1900,
		PipLocation:    -4,
		QuoteToAccount: 1.0,
	}

	got := Calculate(in)

	assert.InDelta(t, 100.0, got.StopPips, 1e-9)
	assert.InDelta(t, 100.0, got.RiskAmount, 1e-9)
	assert.InDelta(t, 10000.0, got.Units, 1.0)
}

func TestCalculate_NonUSDQuoteConversion(t *testing.T) {
	t.Parallel()

	in := Inputs{
		Equity:         5000,
		RiskPct:        0.02,
		EntryPrice:     150.00,
		StopPrice:      149.50,
		PipLocation:    -2,
		QuoteToAccount: 0.0091,
	}

	got := Calculate(in)

	assert.InDelta(t, 50.0, got.StopPips, 1e-9)
	assert.InDelta(t, 100.0, got.RiskAmount, 1e-9)
	assert.InDelta(t, 21978.0, got.Units, 1.0)
}

func TestCalculate_StopAboveEntry(t *testing.T) {
	t.Parallel()

	in := Inputs{
		Equity:         2000,
		RiskPct:        0.005,
		EntryPrice:     1.0000,
		StopPrice:      1.0100,
		PipLocation:    -4,
		QuoteToAccount: 1.0,
	}

	got := Calculate(in)

	assert.InDelta(t, 100.0, got.StopPips, 1e-9)
	assert.InDelta(t, 10.0, got.RiskAmount, 1e-9)
	assert.InDelta(t, 1000.0, got.Units, 1.0)
}
