package emacross

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
)

func mkClose(close float64) market.Candle {
	toP := func(x float64) types.Price { return types.Price(x*float64(types.PriceScale) + 0.5) }
	return market.Candle{Close: toP(close)}
}

func feedSignals(s *Cross, closes []float64) []strategy.Signal {
	out := make([]strategy.Signal, 0, len(closes))
	for _, c := range closes {
		d := s.Update(context.Background(), &market.CandleTime{Candle: mkClose(c)}, nil)
		out = append(out, d)
	}
	return out
}

func TestCross_WarmupNoSignals(t *testing.T) {
	s, err := New(Config{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      types.PriceScale,
	})
	require.NoError(t, err)

	signals := feedSignals(s, []float64{1.0000, 1.0001, 1.0002, 1.0003})
	require.Len(t, signals, 4)
	for _, sig := range signals {
		require.Equal(t, types.Flat, sig.Side)
	}
}

func TestCross_BaselineThenCrossUpThenCrossDown_EmitsOpenSignals(t *testing.T) {
	s, err := New(Config{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      types.PriceScale,
		MinSpread:  0,
	})
	require.NoError(t, err)

	closes := make([]float64, 0, 200)

	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}

	p := 1.0000
	for i := 0; i < 20; i++ {
		p -= 0.0002
		closes = append(closes, p)
	}

	for i := 0; i < 30; i++ {
		p += 0.0003
		closes = append(closes, p)
	}

	for i := 0; i < 30; i++ {
		p -= 0.0003
		closes = append(closes, p)
	}

	signals := feedSignals(s, closes)
	require.NotEmpty(t, signals)

	var directional []strategy.Signal
	for _, sig := range signals {
		if sig.Side != types.Flat {
			directional = append(directional, sig)
		}
	}

	require.Len(t, directional, 2, "expect exactly one long and one short signal")
	require.Equal(t, types.Long, directional[0].Side, "first signal should be long (cross up)")
	require.Equal(t, types.Short, directional[1].Side, "second signal should be short (cross down)")
}

func TestCross_MinSpreadFiltersNoise(t *testing.T) {
	s, err := New(Config{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      types.PriceScale,
		MinSpread:  0.0010,
	})
	require.NoError(t, err)

	closes := make([]float64, 0, 64)
	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}
	closes = append(closes,
		1.0002, 1.0001, 1.0003, 1.0002, 1.0004, 1.0003,
		1.0002, 1.0001, 1.0000, 0.9999, 1.0000, 1.0001,
	)

	signals := feedSignals(s, closes)
	require.NotEmpty(t, signals)
	for _, sig := range signals {
		require.Equal(t, types.Flat, sig.Side)
	}
}

func TestCross_ResetReplaysSameSignalSequence(t *testing.T) {
	cfg := Config{
		FastPeriod: 3,
		SlowPeriod: 5,
		Scale:      types.PriceScale,
		MinSpread:  0,
	}
	s, err := New(cfg)
	require.NoError(t, err)

	closes := make([]float64, 0, 64)
	for i := 0; i < 40; i++ {
		closes = append(closes, 1.0000)
	}
	closes = append(closes,
		1.0002, 1.0004, 1.0006, 1.0008, 1.0010, 1.0012, 1.0014, 1.0016,
		1.0014, 1.0012, 1.0010, 1.0008, 1.0006, 1.0004, 1.0002, 1.0000,
	)

	sigSignature := func(sigs []strategy.Signal) []string {
		var out []string
		for _, s := range sigs {
			out = append(out, fmt.Sprintf("%s side=%v closeAll=%v", s.Reason, s.Side, s.CloseAll))
		}
		return out
	}

	signals1 := feedSignals(s, closes)
	require.NotEmpty(t, signals1)

	s.Reset()

	signals2 := feedSignals(s, closes)
	require.Equal(t, sigSignature(signals1), sigSignature(signals2), "after reset, strategy should emit identical signal sequence")
}

func TestCross_Name(t *testing.T) {
	s, err := New(Config{FastPeriod: 3, SlowPeriod: 5, Scale: types.PriceScale})
	require.NoError(t, err)
	require.Equal(t, "EMA_CROSS(3,5)", s.Name())
}

func TestCross_Reason(t *testing.T) {
	s, err := New(Config{FastPeriod: 3, SlowPeriod: 5, Scale: types.PriceScale})
	require.NoError(t, err)
	sig := s.Update(context.Background(), &market.CandleTime{Candle: mkClose(1.0)}, nil)
	require.NotEmpty(t, sig.Reason)
}

func TestNew_ErrorOnInvalidConfig(t *testing.T) {
	_, err := New(Config{FastPeriod: 0, SlowPeriod: 5, Scale: types.PriceScale})
	require.Error(t, err)
	_, err = New(Config{FastPeriod: 3, SlowPeriod: 0, Scale: types.PriceScale})
	require.Error(t, err)
	_, err = New(Config{FastPeriod: 5, SlowPeriod: 3, Scale: types.PriceScale})
	require.Error(t, err)
	_, err = New(Config{FastPeriod: 3, SlowPeriod: 5, Scale: 0})
	require.Error(t, err)
}
