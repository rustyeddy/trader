package strategies

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

func mkClose(scale types.Scale6, px float64) market.Candle {
	return market.Candle{
		Close: types.Price(px * float64(scale)),
	}
}

func TestTemplateStrategy_Warmup(t *testing.T) {
	s := NewTemplateStrategy(TemplateStrategyConfig{
		Lookback:  3,
		Threshold: 0.0010,
		Scale:     types.PriceScale,
	})

	if s.Ready() {
		t.Fatalf("expected not ready initially")
	}

	for i := 0; i < 2; i++ {
		d := s.Update(mkClose(types.PriceScale, 1.1000))
		if d.Signal() != Hold {
			t.Fatalf("expected Hold during warmup, got %v", d.Signal())
		}
	}

	if s.Ready() {
		t.Fatalf("expected not ready before lookback reached")
	}
}

func TestTemplateStrategy_EmitsSignal(t *testing.T) {
	s := NewTemplateStrategy(TemplateStrategyConfig{
		Lookback:  2,
		Threshold: 0.0010,
		Scale:     types.PriceScale,
	})

	_ = s.Update(mkClose(types.PriceScale, 1.1000))  // warmup
	d := s.Update(mkClose(types.PriceScale, 1.1020)) // up move

	if d.Signal() != Buy {
		t.Fatalf("expected Buy, got %v", d.Signal())
	}
}

func TestTemplateStrategy_Reset(t *testing.T) {
	s := NewTemplateStrategy(TemplateStrategyConfig{
		Lookback:  2,
		Threshold: 0.0010,
		Scale:     types.PriceScale,
	})

	_ = s.Update(mkClose(types.PriceScale, 1.1000))
	_ = s.Update(mkClose(types.PriceScale, 1.1020))

	s.Reset()

	if s.Ready() {
		t.Fatalf("expected not ready after reset")
	}

	d := s.Update(mkClose(types.PriceScale, 1.1000))
	if d.Signal() != Hold {
		t.Fatalf("expected Hold after reset warmup, got %v", d.Signal())
	}
}
