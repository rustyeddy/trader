package strategies

import (
	"testing"

	"github.com/rustyeddy/trader/types"
)

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
		d := s.Update(mkClose(1.1000))
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

	_ = s.Update(mkClose(1.1000))  // warmup
	d := s.Update(mkClose(1.1020)) // up move

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

	_ = s.Update(mkClose(1.1000))
	_ = s.Update(mkClose(1.1020))

	s.Reset()

	if s.Ready() {
		t.Fatalf("expected not ready after reset")
	}

	d := s.Update(mkClose(1.1000))
	if d.Signal() != Hold {
		t.Fatalf("expected Hold after reset warmup, got %v", d.Signal())
	}
}

func TestTemplateStrategy_Name(t *testing.T) {
s := NewTemplateStrategy(TemplateStrategyConfig{
Lookback:  3,
Threshold: 0.0010,
Scale:     types.PriceScale,
})
if s.Name() == "" {
t.Fatalf("expected non-empty name")
}
}

func TestTemplateStrategyDecision_Reason(t *testing.T) {
s := NewTemplateStrategy(TemplateStrategyConfig{
Lookback:  2,
Threshold: 0.0010,
Scale:     types.PriceScale,
})
d := s.Update(mkClose(1.1000))
if d.Reason() == "" {
t.Fatalf("expected non-empty reason")
}
}

func TestTemplateStrategy_SellSignal(t *testing.T) {
s := NewTemplateStrategy(TemplateStrategyConfig{
Lookback:  2,
Threshold: 0.0010,
Scale:     types.PriceScale,
})
_ = s.Update(mkClose(1.1020)) // warmup
d := s.Update(mkClose(1.1000)) // down move exceeds threshold
if d.Signal() != Sell {
t.Fatalf("expected Sell, got %v", d.Signal())
}
}

func TestTemplateStrategy_HoldAfterWarmup(t *testing.T) {
s := NewTemplateStrategy(TemplateStrategyConfig{
Lookback:  2,
Threshold: 0.0100, // large threshold so small moves don't trigger
Scale:     types.PriceScale,
})
_ = s.Update(mkClose(1.1000)) // warmup
d := s.Update(mkClose(1.1001)) // tiny move, below threshold
if d.Signal() != Hold {
t.Fatalf("expected Hold, got %v", d.Signal())
}
}

func TestNewTemplateStrategy_PanicOnInvalidConfig(t *testing.T) {
defer func() {
if r := recover(); r == nil {
t.Fatalf("expected panic on Lookback <= 0")
}
}()
NewTemplateStrategy(TemplateStrategyConfig{
Lookback:  0,
Threshold: 0.001,
Scale:     types.PriceScale,
})
}

func TestNewTemplateStrategy_PanicOnZeroScale(t *testing.T) {
defer func() {
if r := recover(); r == nil {
t.Fatalf("expected panic on Scale <= 0")
}
}()
NewTemplateStrategy(TemplateStrategyConfig{
Lookback:  3,
Threshold: 0.001,
Scale:     0,
})
}
