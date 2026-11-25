package trader

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/require"
)

// Practical financial goldens: amounts below are calculated directly from
// units * (exit - entry), NOT from Trader's P&L helpers. They exercise the
// account ledger with realistic FX prices and a $10,000 starting balance.
// Tiny sub-cent rounding boundaries are covered by the existing num tests;
// they are intentionally not the focus of this regression.
func TestFinancialGolden_FillsMarksAndAccountBalance(t *testing.T) {
	cases := []struct {
		name                               string
		side                               direction
		entry, mark, exit                  string
		unrealized, realized, finalBalance string
	}{
		{"long winner", long, "1.10000", "1.10300", "1.10250", "30", "25", "10025"},
		{"long loser", long, "1.10000", "1.09700", "1.09800", "-30", "-20", "9980"},
		{"short winner", short, "1.10000", "1.09700", "1.09800", "30", "20", "10020"},
		{"short loser", short, "1.10000", "1.10300", "1.10250", "-30", "-25", "9975"},
		// A two-pip adverse round trip from fill prices is a $2 loss
		// for 10,000 units. This models spread/slippage through fills,
		// NOT a separate commission or fee charged by the current engine.
		{"adverse fills", long, "1.10010", "1.09990", "1.09990", "-2", "-2", "9998"},
	}
	money := func(s string) num.Money { return num.MustParseMoney(s, usdCurrency) }
	price := func(s string) num.Price { return num.MustParsePrice(s) }

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newAccount("golden", money("10000"), testMarkets())
			start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
			openOrder := &order{instrument: "EURUSD", action: open, direction: tc.side, units: qty(10000)}
			require.NoError(t, a.applyFill(openOrder, price(tc.entry), start))
			require.Equal(t, money("10000"), a.balance)
			require.Equal(t, money("10000"), must1(a.equity()))

			a.positions["EURUSD"].markPrice = price(tc.mark)
			require.Equal(t, money(tc.unrealized), must1(a.unrealizedPnL()))
			expectedMarkedEquity := must1(money("10000").Add(money(tc.unrealized)))
			require.Equal(t, expectedMarkedEquity, must1(a.equity()))

			closeOrder := &order{instrument: "EURUSD", action: close}
			require.NoError(t, a.applyFill(closeOrder, price(tc.exit), start.Add(time.Hour)))
			require.Equal(t, money(tc.realized), a.trades[0].pnl)
			require.Equal(t, money(tc.finalBalance), a.balance)
			require.Equal(t, money(tc.finalBalance), must1(a.equity()))
			require.Empty(t, a.positions)
			require.Equal(t, qty(10000), closeOrder.units)
		})
	}
}

// This is the underlying peak-to-trough account-equity arithmetic, not an
// assertion about a specific reporter's drawdown metric definition.
func TestFinancialGolden_EquityPathForDrawdownResearch(t *testing.T) {
	a := newAccount("golden", usd(10000), testMarkets())
	entry := &order{instrument: "EURUSD", action: open, direction: long, units: qty(10000)}
	require.NoError(t, a.applyFill(entry, num.MustParsePrice("1.10000"), testStart))

	for _, point := range []struct {
		mark   string
		equity string
	}{
		{"1.10000", "10000"},
		{"1.09700", "9970"}, // $30 peak-to-trough decline, 0.30% of $10,000
		{"1.10200", "10020"},
	} {
		a.positions["EURUSD"].markPrice = num.MustParsePrice(point.mark)
		require.Equal(t, num.MustParseMoney(point.equity, usdCurrency), must1(a.equity()))
	}
}
