package sim

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnrealizedPL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		trade          Trade
		currentPrice   float64
		quoteToAccount float64
		expected       float64
	}{
		{
			name: "long_profit",
			trade: Trade{
				Units:      1000,
				EntryPrice: 1.2000,
			},
			currentPrice:   1.2050,
			quoteToAccount: 1.0,
			expected:       5.0,
		},
		{
			name: "long_loss",
			trade: Trade{
				Units:      1000,
				EntryPrice: 1.2000,
			},
			currentPrice:   1.1900,
			quoteToAccount: 1.0,
			expected:       -10.0,
		},
		{
			name: "short_profit",
			trade: Trade{
				Units:      -1000,
				EntryPrice: 1.2000,
			},
			currentPrice:   1.1900,
			quoteToAccount: 1.0,
			expected:       10.0,
		},
		{
			name: "short_loss",
			trade: Trade{
				Units:      -1000,
				EntryPrice: 1.2000,
			},
			currentPrice:   1.2050,
			quoteToAccount: 1.0,
			expected:       -5.0,
		},
		{
			name: "zero_units",
			trade: Trade{
				Units:      0,
				EntryPrice: 1.2000,
			},
			currentPrice:   1.2500,
			quoteToAccount: 1.0,
			expected:       0.0,
		},
		{
			name: "scaled_quote_to_account",
			trade: Trade{
				Units:      1000,
				EntryPrice: 150.00,
			},
			currentPrice:   150.50,
			quoteToAccount: 0.0091,
			expected:       4.55,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := UnrealizedPL(tt.trade, tt.currentPrice, tt.quoteToAccount)
			assert.InDelta(t, tt.expected, got, 1e-9)
		})
	}
}
