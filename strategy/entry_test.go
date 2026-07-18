package strategy

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEntryTrigger_EmptyKindReturnsNextOpen(t *testing.T) {
	t.Parallel()
	tr, err := GetEntryTrigger(EntryConfig{}, types.PriceScale)
	require.NoError(t, err)
	assert.Equal(t, "next-open", tr.Name())
}

func TestGetEntryTrigger_ExplicitNextOpen(t *testing.T) {
	t.Parallel()
	tr, err := GetEntryTrigger(EntryConfig{Kind: "next-open"}, types.PriceScale)
	require.NoError(t, err)
	assert.Equal(t, "next-open", tr.Name())
}

func TestGetEntryTrigger_RejectionCandle(t *testing.T) {
	t.Parallel()
	tr, err := GetEntryTrigger(EntryConfig{Kind: "rejection-candle"}, types.PriceScale)
	require.NoError(t, err)
	_, ok := tr.(*WickRejectionEntry)
	assert.True(t, ok)
}

func TestGetEntryTrigger_RejectionCandleWithParams(t *testing.T) {
	t.Parallel()
	tr, err := GetEntryTrigger(EntryConfig{
		Kind: "rejection-candle",
		Params: map[string]any{
			"min-wick-ratio": 0.6,
			"lookback":       int64(2),
		},
	}, types.PriceScale)
	require.NoError(t, err)
	wr, ok := tr.(*WickRejectionEntry)
	require.True(t, ok)
	assert.Equal(t, 2, wr.lookback)
}

func TestGetEntryTrigger_UnknownKind(t *testing.T) {
	t.Parallel()
	_, err := GetEntryTrigger(EntryConfig{Kind: "bogus"}, types.PriceScale)
	assert.ErrorContains(t, err, "bogus")
}

func TestGetEntryTrigger_InvalidParamPropagatesError(t *testing.T) {
	t.Parallel()
	_, err := GetEntryTrigger(EntryConfig{
		Kind:   "rejection-candle",
		Params: map[string]any{"min-wick-ratio": 5.0}, // out of (0,1]
	}, types.PriceScale)
	assert.Error(t, err)
}

// ── NextOpenEntry ────────────────────────────────────────────────────────────

func TestNextOpenEntry_AlwaysReadyAndTriggered(t *testing.T) {
	t.Parallel()
	var tr NextOpenEntry
	assert.True(t, tr.Ready())
	assert.True(t, tr.Triggered(types.Long, time.Now(), market.Candle{}))
	assert.True(t, tr.Triggered(types.Short, time.Now(), market.Candle{}))
	assert.NotPanics(t, func() {
		tr.Tick(market.Candle{})
		tr.Reset()
	})
}
