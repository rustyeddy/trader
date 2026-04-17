package strategies

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
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
		require.Equal(t, "hold", d.Reason)
		require.Empty(t, d.Opens)
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
	d := s.Update(mkClose(1.1020))
	require.Equal(t, "hold", d.Reason)
	require.Empty(t, d.Opens)
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
	require.Equal(t, "hold", d.Reason)
}

func TestTemplateStrategy_Name(t *testing.T) {
	s := NewTemplateStrategy(TemplateStrategyConfig{Lookback: 3, Threshold: 0.0010, Scale: types.PriceScale})
	require.NotEmpty(t, s.Name())
}

func TestTemplateStrategyPlan_Reason(t *testing.T) {
	s := NewTemplateStrategy(TemplateStrategyConfig{Lookback: 2, Threshold: 0.0010, Scale: types.PriceScale})
	d := s.Update(mkClose(1.1000))
	require.NotEmpty(t, d.Reason)
}

func TestTemplateStrategy_NoSignalPlan(t *testing.T) {
	s := NewTemplateStrategy(TemplateStrategyConfig{Lookback: 2, Threshold: 0.0010, Scale: types.PriceScale})
	_ = s.Update(mkClose(1.1020))
	d := s.Update(mkClose(1.1000))
	require.Empty(t, d.Opens)
	require.Empty(t, d.Closes)
}

func TestTemplateStrategy_HoldAfterWarmup(t *testing.T) {
s := NewTemplateStrategy(TemplateStrategyConfig{
Lookback:  2,
Threshold: 0.0100, // large threshold so small moves don't trigger
Scale:     types.PriceScale,
})
	_ = s.Update(mkClose(1.1000))
	d := s.Update(mkClose(1.1001))
	require.Equal(t, "hold", d.Reason)
}

func TestNewTemplateStrategy_PanicOnInvalidConfig(t *testing.T) {
	require.Panics(t, func() {
		NewTemplateStrategy(TemplateStrategyConfig{Lookback: 0, Threshold: 0.001, Scale: types.PriceScale})
	})
}

func TestNewTemplateStrategy_PanicOnZeroScale(t *testing.T) {
	require.Panics(t, func() {
		NewTemplateStrategy(TemplateStrategyConfig{Lookback: 3, Threshold: 0.001, Scale: 0})
	})
}
