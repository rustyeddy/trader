package sim

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
)

func TestUnrealizedPL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		trade          Trade
		currentPrice   types.Price
		quoteToAccount types.Rate
		expected       types.Money
	}{
		{
			name: "long_profit",
			trade: Trade{
				Units:      1000,
				EntryPrice: types.PriceFromFloat(1.2000),
			},
			currentPrice:   types.PriceFromFloat(1.2050),
			quoteToAccount: types.RateScale,
			expected:       types.Money(int64(types.Units(1000) * types.Units(types.PriceFromFloat(1.2050)-types.PriceFromFloat(1.2000))) * int64(types.RateScale)),
		},
		{
			name: "long_loss",
			trade: Trade{
				Units:      1000,
				EntryPrice: types.PriceFromFloat(1.2000),
			},
			currentPrice:   types.PriceFromFloat(1.1900),
			quoteToAccount: types.RateScale,
			expected:       types.Money(int64(types.Units(1000) * types.Units(types.PriceFromFloat(1.1900)-types.PriceFromFloat(1.2000))) * int64(types.RateScale)),
		},
		{
			name: "short_profit",
			trade: Trade{
				Units:      -1000,
				EntryPrice: types.PriceFromFloat(1.2000),
			},
			currentPrice:   types.PriceFromFloat(1.1900),
			quoteToAccount: types.RateScale,
			expected:       types.Money(int64(types.Units(-1000) * types.Units(types.PriceFromFloat(1.1900)-types.PriceFromFloat(1.2000))) * int64(types.RateScale)),
		},
		{
			name: "short_loss",
			trade: Trade{
				Units:      -1000,
				EntryPrice: types.PriceFromFloat(1.2000),
			},
			currentPrice:   types.PriceFromFloat(1.2050),
			quoteToAccount: types.RateScale,
			expected:       types.Money(int64(types.Units(-1000) * types.Units(types.PriceFromFloat(1.2050)-types.PriceFromFloat(1.2000))) * int64(types.RateScale)),
		},
		{
			name: "zero_units",
			trade: Trade{
				Units:      0,
				EntryPrice: types.PriceFromFloat(1.2000),
			},
			currentPrice:   types.PriceFromFloat(1.2500),
			quoteToAccount: types.RateScale,
			expected:       0,
		},
		{
			name: "scaled_quote_to_account",
			trade: Trade{
				Units:      1000,
				EntryPrice: types.PriceFromFloat(150.00),
			},
			currentPrice:   types.PriceFromFloat(150.50),
			quoteToAccount: types.RateFromFloat(0.0091),
			expected:       types.Money(int64(types.Units(1000) * types.Units(types.PriceFromFloat(150.50)-types.PriceFromFloat(150.00))) * int64(types.RateFromFloat(0.0091))),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := UnrealizedPL(tt.trade, tt.currentPrice, tt.quoteToAccount)
			assert.Equal(t, tt.expected, got)
		})
	}
}
