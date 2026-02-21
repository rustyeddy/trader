package broker

import (
	"testing"

	"github.com/rustyeddy/trader/market"
)

func TestPriceMid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		bid      market.Price
		ask      market.Price
		expected market.Price
	}{
		{"simple", 10, 30, 20},
		{"same", 25, 25, 25},
		{"zero", 00, 00, 00},
		{"negative", -20, 20, 00},
		{"fractional", 11, 13, 12},
	}

	const tol = 0

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := market.Tick{
				BA: market.BA{Bid: tt.bid, Ask: tt.ask},
			}
			got := p.Mid()
			v := got - tt.expected
			if v < 0 {
				v = -v
			}

			if v > tol {
				t.Fatalf("Mid() = %v, expected %v", got, tt.expected)
			}
		})
	}
}
