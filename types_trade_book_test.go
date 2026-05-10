package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradeBookGet_FoundAndMissing_Phase1(t *testing.T) {
	t.Parallel()

	tr := &Trade{TradeCommon: &TradeCommon{ID: "abc"}}
	tb := &tradeBook{Trades: map[string]*Trade{"abc": tr}}

	got := tb.Get("abc")
	require.NotNil(t, got)
	assert.Same(t, tr, got)

	missing := tb.Get("nope")
	assert.Nil(t, missing)
}
