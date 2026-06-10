package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPosition(inst string, side Side, units Units, fill float64) *Lot {
	th := NewTradeHistory(inst)
	th.Side = side
	th.Units = units
	return &Lot{
		TradeCommon:    th.TradeCommon,
		EntryPrice:     PriceFromFloat(fill),
		EntryTime:      Timestamp(1000),
		OriginalUnits:  units,
		RemainingUnits: units,
		State:          LotOpen,
	}
}

func TestAccountResolveAndResolveWithMarks(t *testing.T) {
	t.Parallel()

	acct := NewAccount("acct", MoneyFromFloat(10_000))
	pos := newTestPosition("EURUSD", Long, 100_000, 1.1000)

	require.NoError(t, acct.AddLot(pos))
	require.Equal(t, 1, acct.Lots.Len())

	require.NoError(t, acct.ResolveWithMarks(nil))
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
	require.Error(t, nilAcct.ResolveWithMarks(nil))

	acct := NewAccount("acct", MoneyFromFloat(10_000))
	pos := newTestPosition("EURUSD", Long, 100_000, 1.1000)
	acct.Lots.Add(pos)

	err := acct.ResolveWithMarks(map[string]Price{"EURUSD": 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mark")

	acct.Lots = LotBook{}
	badUnits := newTestPosition("EURUSD", Long, 0, 1.1000)
	badUnits.RemainingUnits = 0
	acct.Lots.Add(badUnits)
	err = acct.ResolveWithMarks(map[string]Price{"EURUSD": PriceFromFloat(1.1010)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid units")

	acct.Lots = LotBook{}
	badPrice := newTestPosition("EURUSD", Long, 100_000, 1.1000)
	badPrice.EntryPrice = 0
	acct.Lots.Add(badPrice)
	err = acct.ResolveWithMarks(map[string]Price{"EURUSD": PriceFromFloat(1.1010)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry price")
}

func TestAccountAddPositionValidation(t *testing.T) {
	t.Parallel()

	pos := newTestPosition("EURUSD", Long, 100_000, 1.1000)

	var nilAcct *Account
	err := nilAcct.AddLot(pos)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account is nil")

	acct := NewAccount("acct", MoneyFromFloat(10_000))

	badInstrument := newTestPosition("", Long, 100_000, 1.1000)
	err = acct.AddLot(badInstrument)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instrument")

	badUnits := newTestPosition("EURUSD", Long, 0, 1.1000)
	badUnits.Units = 0
	err = acct.AddLot(badUnits)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "units")

	badPrice := newTestPosition("EURUSD", Long, 100_000, 0)
	err = acct.AddLot(badPrice)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "price")

	emptyID := newTestPosition("EURUSD", Long, 100_000, 1.1000)
	emptyID.ID = ""
	err = acct.AddLot(emptyID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id")
}

func TestAccountRealizePNLAndClosePosition(t *testing.T) {
	t.Parallel()

	acct := NewAccount("acct", MoneyFromFloat(10_000))

	tests := []struct {
		name     string
		position *Lot
		trade    *Trade
		wantPNL  Money
	}{
		{
			name:     "long gain",
			position: newTestPosition("EURUSD", Long, 100_000, 1.1000),
			trade:    &Trade{TradeCommon: NewTradeHistory("EURUSD").TradeCommon, ExitPrice: PriceFromFloat(1.1010), ExitTime: Timestamp(2000)},
			wantPNL:  MoneyFromFloat(100),
		},
		{
			name:     "short gain",
			position: newTestPosition("EURUSD", Short, 100_000, 1.1000),
			trade:    &Trade{TradeCommon: NewTradeHistory("EURUSD").TradeCommon, ExitPrice: PriceFromFloat(1.0990), ExitTime: Timestamp(2000)},
			wantPNL:  MoneyFromFloat(100),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			acct.Balance = MoneyFromFloat(10_000)
			acct.Equity = MoneyFromFloat(10_000)
			acct.Trades = nil
			acct.Lots = LotBook{}

			pnl, err := acct.realizePNL(tt.position, tt.trade)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPNL, pnl)
			assert.Equal(t, MoneyFromFloat(10_000)+tt.wantPNL, acct.Balance)
			assert.Equal(t, acct.Balance, acct.Equity)
		})
	}

	_, err := acct.realizePNL(nil, &Trade{})
	require.Error(t, err)

	_, err = acct.realizePNL(newTestPosition("EURUSD", Long, 100_000, 1.1000), nil)
	require.Error(t, err)

	_, err = acct.realizePNL(newTestPosition("", Long, 100_000, 1.1000), &Trade{ExitPrice: PriceFromFloat(1.1010)})
	require.Error(t, err)

	_, err = acct.realizePNL(newTestPosition("EURUSD", Long, 0, 1.1000), &Trade{ExitPrice: PriceFromFloat(1.1010)})
	require.Error(t, err)

	_, err = acct.realizePNL(newTestPosition("EURUSD", Long, 100_000, 1.1000), &Trade{ExitPrice: 0})
	require.Error(t, err)
}

func TestAccountClosePositionAndPlaceholderClosePosition(t *testing.T) {
	t.Parallel()

	acct := NewAccount("acct", MoneyFromFloat(10_000))
	pos := newTestPosition("EURUSD", Long, 100_000, 1.1000)
	acct.Lots.Add(pos)
	trade := &Trade{TradeCommon: pos.TradeCommon, ExitPrice: PriceFromFloat(1.1010), ExitTime: Timestamp(2000)}

	require.NoError(t, acct.CloseLot(pos, trade))
	assert.Equal(t, 0, acct.Lots.Len())
	require.Len(t, acct.Trades, 1)
	assert.Equal(t, MoneyFromFloat(100), trade.PNL)
	assert.Equal(t, MoneyFromFloat(10_100), acct.Balance)

	var nilAcct *Account
	var err error
	assert.NotPanics(t, func() {
		err = nilAcct.CloseLot(pos, trade)
	})
	require.Error(t, err)

	acct2 := NewAccount("acct2", MoneyFromFloat(10_000))
	assert.Error(t, acct2.CloseLot(&Lot{TradeCommon: &TradeCommon{ID: NewULID()}}, trade))
	assert.Error(t, acct2.CloseLot(newTestPosition("EURUSD", Long, 100_000, 1.1000), &Trade{}))
}
