package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewOpenRequest_PopulatesFieldsFromCandleAndArgs_Phase1 verifies expected behavior for this component.
func TestNewOpenRequest_PopulatesFieldsFromCandleAndArgs_Phase1(t *testing.T) {
	t.Parallel()

	ct := &CandleTime{
		Candle: Candle{
			Open:  PriceFromFloat(1.1000),
			High:  PriceFromFloat(1.1010),
			Low:   PriceFromFloat(1.0990),
			Close: PriceFromFloat(1.1005),
			Ticks: 42,
		},
		Timestamp: FromString("2024-01-15"),
	}

	op := NewOpenRequest(
		"EURUSD",
		ct,
		Long,
		PriceFromFloat(1.0950),
		PriceFromFloat(1.1050),
		"phase1-open",
	)

	require.NotNil(t, op)
	require.NotNil(t, op.TradeCommon)

	assert.Equal(t, RequestMarketOpen, op.RequestType)
	assert.Equal(t, ct.Close, op.Price)
	assert.Equal(t, ct.Timestamp, op.Timestamp)
	assert.Equal(t, ct.Candle, op.Candle)
	assert.Equal(t, "phase1-open", op.Reason)

	assert.Equal(t, "EURUSD", op.Instrument)
	assert.Equal(t, Long, op.Side)
	assert.Equal(t, PriceFromFloat(1.0950), op.Stop)
	assert.Equal(t, PriceFromFloat(1.1050), op.Take)
	assert.NotEmpty(t, op.ID)
}

// TestCloseCauseString_AllValues_Phase1 verifies expected behavior for this component.
func TestCloseCauseString_AllValues_Phase1(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   closeCause
		want string
	}{
		{CloseUnknown, "Unknown"},
		{CloseManual, "Manual"},
		{CloseStopLoss, "StopLoss"},
		{CloseTakeProfit, "TakeProfit"},
		{CloseBrokerLiquidation, "BrokerLiquidation"},
		{closeCause(255), "Unknown"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.in.String())
	}
}
