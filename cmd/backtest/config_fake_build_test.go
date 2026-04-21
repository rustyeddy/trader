package backtest

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFakeStrategy_OpensOnlyOnce(t *testing.T) {
	s := newConfigFakeStrategy("EURUSD")

	first := s.Update(context.Background(), &trader.CandleTime{Candle: trader.Candle{Close: trader.Price(1100000)}}, nil)
	require.NotNil(t, first)
	require.Len(t, first.Opens, 1)
	assert.Equal(t, "fake-open", first.Reason)
	assert.Equal(t, "EURUSD", first.Opens[0].Instrument)
	assert.Equal(t, trader.Long, first.Opens[0].Side)

	second := s.Update(context.Background(), &trader.CandleTime{Candle: trader.Candle{Close: trader.Price(1100100)}}, nil)
	require.NotNil(t, second)
	assert.Empty(t, second.Opens)
}

func TestConfigFakeStrategy_Reset(t *testing.T) {
	s := newConfigFakeStrategy("EURUSD")

	_ = s.Update(context.Background(), &trader.CandleTime{Candle: trader.Candle{Close: trader.Price(1100000)}}, nil)
	s.Reset()
	afterReset := s.Update(context.Background(), &trader.CandleTime{Candle: trader.Candle{Close: trader.Price(1100100)}}, nil)
	require.NotNil(t, afterReset)
	require.Len(t, afterReset.Opens, 1)
}
