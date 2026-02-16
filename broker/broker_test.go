package broker

import (
	"math"
	"testing"

	"github.com/rustyeddy/trader/market"
)

func TestPriceMid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		bid      float64
		ask      float64
		expected float64
	}{
		{"simple", 1.0, 3.0, 2.0},
		{"same", 2.5, 2.5, 2.5},
		{"zero", 0.0, 0.0, 0.0},
		{"negative", -2.0, 2.0, 0.0},
		{"fractional", 1.1, 1.3, 1.2},
	}

	const tol = 1e-9

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := market.Tick{Bid: tt.bid, Ask: tt.ask}
			got := p.Mid()
			if math.Abs(got-tt.expected) > tol {
				t.Fatalf("Mid() = %v, expected %v", got, tt.expected)
			}
		})
	}
}
