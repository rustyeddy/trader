package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStrategyFromResolvedRun_KnownKinds(t *testing.T) {
	tests := []struct {
		name string
		rr   *ResolvedRun
	}{
		{
			name: "fake",
			rr: &ResolvedRun{
				Instrument: "EURUSD",
				Strategy:   StrategyConfig{Kind: "fake"},
			},
		},
		{
			name: "fake-02",
			rr: &ResolvedRun{
				Instrument: "EURUSD",
				Strategy:   StrategyConfig{Kind: "fake-02"},
			},
		},
		{
			name: "noop",
			rr: &ResolvedRun{
				Instrument: "EURUSD",
				Strategy:   StrategyConfig{Kind: "noop"},
			},
		},
		{
			name: "ema-cross",
			rr: &ResolvedRun{
				Instrument: "EURUSD",
				Scale:      PriceScale,
				Strategy: StrategyConfig{
					Kind: "ema-cross",
					Params: map[string]any{
						"fast": 12,
						"slow": 26,
					},
				},
			},
		},
		{
			name: "ema-cross-adx",
			rr: &ResolvedRun{
				Instrument: "EURUSD",
				Scale:      PriceScale,
				Strategy: StrategyConfig{
					Kind: "ema-cross-adx",
					Params: map[string]any{
						"fast": 12,
						"slow": 26,
					},
				},
			},
		},
		{
			name: "template",
			rr: &ResolvedRun{
				Instrument: "EURUSD",
				Scale:      PriceScale,
				Strategy: StrategyConfig{
					Kind: "template",
					Params: map[string]any{
						"lookback":  10,
						"threshold": 0.1,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strat, err := NewStrategyFromResolvedRun(tt.rr)
			require.NoError(t, err)
			require.NotNil(t, strat)
			assert.NotEmpty(t, strat.Name())
		})
	}
}

func TestNewStrategyFromResolvedRun_UnknownKind(t *testing.T) {
	_, err := NewStrategyFromResolvedRun(&ResolvedRun{
		Instrument: "EURUSD",
		Strategy:   StrategyConfig{Kind: "does-not-exist"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported strategy.kind")
}
