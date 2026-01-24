package sim

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func floatPtr(v float64) *float64 {
	return &v
}

func TestHitStopLoss(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		trade Trade
		price float64
		want  bool
	}{
		{
			name:  "nil_stop_loss",
			trade: Trade{Units: 1000, StopLoss: nil},
			price: 1.0,
			want:  false,
		},
		{
			name:  "long_hit",
			trade: Trade{Units: 1000, StopLoss: floatPtr(1.2)},
			price: 1.2,
			want:  true,
		},
		{
			name:  "long_not_hit",
			trade: Trade{Units: 1000, StopLoss: floatPtr(1.2)},
			price: 1.2001,
			want:  false,
		},
		{
			name:  "short_hit",
			trade: Trade{Units: -1000, StopLoss: floatPtr(1.2)},
			price: 1.2,
			want:  true,
		},
		{
			name:  "short_not_hit",
			trade: Trade{Units: -1000, StopLoss: floatPtr(1.2)},
			price: 1.1999,
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
		price float64
		want  bool
	}{
		{
			name:  "nil_take_profit",
			trade: Trade{Units: 1000, TakeProfit: nil},
			price: 1.0,
			want:  false,
		},
		{
			name:  "long_hit",
			trade: Trade{Units: 1000, TakeProfit: floatPtr(1.2)},
			price: 1.2,
			want:  true,
		},
		{
			name:  "long_not_hit",
			trade: Trade{Units: 1000, TakeProfit: floatPtr(1.2)},
			price: 1.1999,
			want:  false,
		},
		{
			name:  "short_hit",
			trade: Trade{Units: -1000, TakeProfit: floatPtr(1.2)},
			price: 1.2,
			want:  true,
		},
		{
			name:  "short_not_hit",
			trade: Trade{Units: -1000, TakeProfit: floatPtr(1.2)},
			price: 1.2001,
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
