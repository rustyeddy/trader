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

func TestNewOpenRequest_PanicsOnNilCandle(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "NewOpenRequest: candle time is nil", func() {
		NewOpenRequest("EURUSD", nil, Long, 0, 0, "panic")
	})
}

func TestRequestTypeString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "none", RequestNone.String())
	assert.Equal(t, "market-open", RequestMarketOpen.String())
	assert.Equal(t, "limit-open", RequestLimitOpen.String())
	assert.Equal(t, "close", RequestClose.String())
	assert.Equal(t, "unknown", RequestType(99).String())
}

func TestOpenRequestValidate(t *testing.T) {
	t.Parallel()

	require.EqualError(t, (*OpenRequest)(nil).Validate(), "open request is nil")

	req := &OpenRequest{}
	require.EqualError(t, req.Validate(), "open request missing trade common")

	req.TradeCommon = &TradeCommon{ID: "id", Side: Long, Units: 1000}
	req.Price = PriceFromFloat(1.1000)
	require.EqualError(t, req.Validate(), "open request instrument must not be empty")

	req.Instrument = "EURUSD"
	req.Side = Side(0)
	require.EqualError(t, req.Validate(), "open request side must be long or short")

	req.Side = Long
	req.Units = 0
	require.EqualError(t, req.Validate(), "open request units must be > 0")

	req.Units = 1000
	req.Price = 0
	require.EqualError(t, req.Validate(), "open request price must be > 0")
}

func TestCloseRequestValidate(t *testing.T) {
	t.Parallel()

	require.EqualError(t, (*CloseRequest)(nil).Validate(), "close request is nil")

	req := &CloseRequest{}
	require.EqualError(t, req.Validate(), "close request missing position")

	req.Lot = &Lot{}
	require.EqualError(t, req.Validate(), "close request position missing trade common")

	req.Lot.TradeCommon = &TradeCommon{ID: "lot-1", Instrument: "EURUSD", Side: Long, Units: 1000}
	req.Price = PriceFromFloat(1.1000)
	req.Request.TradeCommon = &TradeCommon{ID: "lot-2", Instrument: "EURUSD"}
	require.EqualError(t, req.Validate(), `close request id "lot-2" does not match position id "lot-1"`)

	req.Request.ID = "lot-1"
	req.Request.Instrument = "GBPUSD"
	require.EqualError(t, req.Validate(), `close request instrument "GBPUSD" does not match position instrument "EURUSD"`)
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
