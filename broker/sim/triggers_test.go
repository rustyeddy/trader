package sim

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
)

func pricePtr(v float64) *types.Price {
	p := types.PriceFromFloat(v)
	return &p
}

func TestHitStopLoss(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		trade Trade
		price types.Price
		want  bool
	}{
		{
			name:  "nil_stop_loss",
			trade: Trade{Units: 1000, StopLoss: nil},
			price: types.PriceFromFloat(1.0),
			want:  false,
		},
		{
			name:  "long_hit",
			trade: Trade{Units: 1000, StopLoss: pricePtr(1.2)},
			price: types.PriceFromFloat(1.2),
			want:  true,
		},
		{
			name:  "long_not_hit",
			trade: Trade{Units: 1000, StopLoss: pricePtr(1.2)},
			price: types.PriceFromFloat(1.2001),
			want:  false,
		},
		{
			name:  "short_hit",
			trade: Trade{Units: -1000, StopLoss: pricePtr(1.2)},
			price: types.PriceFromFloat(1.2),
			want:  true,
		},
		{
			name:  "short_not_hit",
			trade: Trade{Units: -1000, StopLoss: pricePtr(1.2)},
			price: types.PriceFromFloat(1.1999),
			want:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, hitStopLoss(&tt.trade, tt.price))
		})
	}
}

func TestHitTakeProfit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		trade Trade
		price types.Price
		want  bool
	}{
		{
			name:  "nil_take_profit",
			trade: Trade{Units: 1000, TakeProfit: nil},
			price: types.PriceFromFloat(1.0),
			want:  false,
		},
		{
			name:  "long_hit",
			trade: Trade{Units: 1000, TakeProfit: pricePtr(1.2)},
			price: types.PriceFromFloat(1.2),
			want:  true,
		},
		{
			name:  "long_not_hit",
			trade: Trade{Units: 1000, TakeProfit: pricePtr(1.2)},
			price: types.PriceFromFloat(1.1999),
			want:  false,
		},
		{
			name:  "short_hit",
			trade: Trade{Units: -1000, TakeProfit: pricePtr(1.2)},
			price: types.PriceFromFloat(1.2),
			want:  true,
		},
		{
			name:  "short_not_hit",
			trade: Trade{Units: -1000, TakeProfit: pricePtr(1.2)},
			price: types.PriceFromFloat(1.2001),
			want:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, hitTakeProfit(&tt.trade, tt.price))
		})
	}
}
