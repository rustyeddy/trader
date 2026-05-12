package trader

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPosition(inst string, side Side, units Units, fill float64) *Position {
	th := NewTradeHistory(inst)
	th.Side = side
	th.Units = units
	return &Position{
		TradeCommon: th.TradeCommon,
		FillPrice:   PriceFromFloat(fill),
		FillTime:    Timestamp(1000),
		State:       PositionOpen,
	}
}

func TestAccountResolveAndResolveWithMarks(t *testing.T) {
	t.Parallel()

	acct := NewAccount("acct", MoneyFromFloat(10_000))
	pos := newTestPosition("EURUSD", Long, 100_000, 1.1000)

	require.NoError(t, acct.AddPosition(context.Background(), pos))
	require.Equal(t, 1, acct.Positions.Len())

	require.NoError(t, acct.Resolve())
	assert.Equal(t, acct.Balance, acct.Equity)
	assert.Greater(t, acct.MarginUsed, Money(0))
	assert.Equal(t, acct.Equity-acct.MarginUsed, acct.FreeMargin)

	require.NoError(t, acct.ResolveWithMarks(map[string]Price{
		"EURUSD": PriceFromFloat(1.1010),
	}))
	assert.Greater(t, acct.Equity, acct.Balance)
	assert.Greater(t, acct.MarginUsed, Money(0))
	assert.Equal(t, acct.Equity-acct.MarginUsed, acct.FreeMargin)
	assert.Greater(t, acct.MarginLevel, Money(0))
}

func TestAccountResolveWithMarksValidation(t *testing.T) {
	t.Parallel()

	var nilAcct *Account
	require.Error(t, nilAcct.Resolve())

	acct := NewAccount("acct", MoneyFromFloat(10_000))
	pos := newTestPosition("EURUSD", Long, 100_000, 1.1000)
	acct.Positions.Add(pos)

	err := acct.ResolveWithMarks(map[string]Price{"EURUSD": 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mark")

	acct.Positions = Positions{}
	badUnits := newTestPosition("EURUSD", Long, 0, 1.1000)
	acct.Positions.Add(badUnits)
	err = acct.ResolveWithMarks(map[string]Price{"EURUSD": PriceFromFloat(1.1010)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid units")

	acct.Positions = Positions{}
	badPrice := newTestPosition("EURUSD", Long, 100_000, 1.1000)
	badPrice.FillPrice = 0
	acct.Positions.Add(badPrice)
	err = acct.ResolveWithMarks(map[string]Price{"EURUSD": PriceFromFloat(1.1010)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry price")
}

func TestAccountAddPositionValidation(t *testing.T) {
	t.Parallel()

	pos := newTestPosition("EURUSD", Long, 100_000, 1.1000)

	var nilAcct *Account
	err := nilAcct.AddPosition(context.Background(), pos)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil account")

	acct := NewAccount("acct", MoneyFromFloat(10_000))

	badInstrument := newTestPosition("", Long, 100_000, 1.1000)
	err = acct.AddPosition(context.Background(), badInstrument)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instrument")

	badUnits := newTestPosition("EURUSD", Long, 0, 1.1000)
	err = acct.AddPosition(context.Background(), badUnits)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "units")

	badPrice := newTestPosition("EURUSD", Long, 100_000, 0)
	err = acct.AddPosition(context.Background(), badPrice)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "price")

	emptyID := newTestPosition("EURUSD", Long, 100_000, 1.1000)
	emptyID.ID = ""
	assert.Panics(t, func() {
		_ = acct.AddPosition(context.Background(), emptyID)
	})
}

func TestAccountRealizePNLAndClosePosition(t *testing.T) {
	t.Parallel()

	acct := NewAccount("acct", MoneyFromFloat(10_000))

	tests := []struct {
		name     string
		position *Position
		trade    *Trade
		wantPNL  Money
	}{
		{
			name:     "long gain",
			position: newTestPosition("EURUSD", Long, 100_000, 1.1000),
			trade:    &Trade{TradeCommon: NewTradeHistory("EURUSD").TradeCommon, FillPrice: PriceFromFloat(1.1010), FillTime: Timestamp(2000)},
			wantPNL:  MoneyFromFloat(100),
		},
		{
			name:     "short gain",
			position: newTestPosition("EURUSD", Short, 100_000, 1.1000),
			trade:    &Trade{TradeCommon: NewTradeHistory("EURUSD").TradeCommon, FillPrice: PriceFromFloat(1.0990), FillTime: Timestamp(2000)},
			wantPNL:  MoneyFromFloat(100),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			acct.Balance = MoneyFromFloat(10_000)
			acct.Equity = MoneyFromFloat(10_000)
			acct.Trades = nil
			acct.Positions = Positions{}

			pnl, err := acct.RealizePNL(tt.position, tt.trade)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPNL, pnl)
			assert.Equal(t, MoneyFromFloat(10_000)+tt.wantPNL, acct.Balance)
			assert.Equal(t, acct.Balance, acct.Equity)
		})
	}

	_, err := acct.RealizePNL(nil, &Trade{})
	require.Error(t, err)

	_, err = acct.RealizePNL(newTestPosition("EURUSD", Long, 100_000, 1.1000), nil)
	require.Error(t, err)

	_, err = acct.RealizePNL(newTestPosition("", Long, 100_000, 1.1000), &Trade{FillPrice: PriceFromFloat(1.1010)})
	require.Error(t, err)

	_, err = acct.RealizePNL(newTestPosition("EURUSD", Long, 0, 1.1000), &Trade{FillPrice: PriceFromFloat(1.1010)})
	require.Error(t, err)

	_, err = acct.RealizePNL(newTestPosition("EURUSD", Long, 100_000, 1.1000), &Trade{FillPrice: 0})
	require.Error(t, err)
}

func TestAccountClosePositionAndPlaceholderClosePosition(t *testing.T) {
	t.Parallel()

	acct := NewAccount("acct", MoneyFromFloat(10_000))
	pos := newTestPosition("EURUSD", Long, 100_000, 1.1000)
	acct.Positions.Add(pos)
	trade := &Trade{TradeCommon: pos.TradeCommon, FillPrice: PriceFromFloat(1.1010), FillTime: Timestamp(2000)}

	require.NoError(t, acct.ClosePosition(pos, trade))
	assert.Equal(t, 0, acct.Positions.Len())
	require.Len(t, acct.Trades, 1)
	assert.Equal(t, MoneyFromFloat(100), trade.PNL)
	assert.Equal(t, MoneyFromFloat(10_100), acct.Balance)

	var nilAcct *Account
	assert.NotPanics(t, func() {
		nilAcct.closePosition(Timestamp(time.Now().Unix()), PriceFromFloat(1.1000), "test")
	})

	var err error
	assert.NotPanics(t, func() {
		err = nilAcct.ClosePosition(pos, trade)
	})
	require.Error(t, err)

	acct2 := NewAccount("acct2", MoneyFromFloat(10_000))
	assert.Error(t, acct2.ClosePosition(&Position{TradeCommon: &TradeCommon{ID: NewULID()}}, trade))
	assert.Error(t, acct2.ClosePosition(newTestPosition("EURUSD", Long, 100_000, 1.1000), &Trade{}))
}

func TestAccountTradeMarginMethod(t *testing.T) {
	t.Parallel()

	acct := NewAccount("acct", MoneyFromFloat(10_000))

	got, err := acct.TradeMargin(100_000, PriceFromFloat(1.1000), "EURUSD")
	require.NoError(t, err)
	assert.Greater(t, got, Money(0))

	_, err = acct.TradeMargin(100_000, 0, "EURUSD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "price")

	_, err = acct.TradeMargin(100_000, PriceFromFloat(1.1000), "XXXYYY")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such instrument")
}
