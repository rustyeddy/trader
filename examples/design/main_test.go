package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// approvedSummary is the tables in emax-validation.org, written out by hand as
// the example prints them. Pending counts the waiting stops of open positions
// as well as waiting opens.
const approvedSummary = `a-reversal-exit: 10 bars
  orders: submitted 4, filled 2, rejected 0, cancelled 1, pending 1
  trades: closed 1, wins 1, losses 0, break-even 0
  money: realized $33.00, unrealized $0.00, balance $10033.00, equity $10033.00
  open: none
b1-stop-wick: 8 bars
  orders: submitted 2, filled 2, rejected 0, cancelled 0, pending 0
  trades: closed 1, wins 0, losses 1, break-even 0
  money: realized -$102.09, unrealized $0.00, balance $9897.91, equity $9897.91
  open: none
b2-stop-cross: 9 bars
  orders: submitted 5, filled 3, rejected 1, cancelled 0, pending 1
  trades: closed 1, wins 0, losses 1, break-even 0
  money: realized -$102.09, unrealized $35.57, balance $9897.91, equity $9933.48
  open: short 9122 EURUSD from 1.08490, marked 1.08100
b3-stop-gap: 7 bars
  orders: submitted 2, filled 2, rejected 0, cancelled 0, pending 0
  trades: closed 1, wins 0, losses 1, break-even 0
  money: realized -$129.01, unrealized $0.00, balance $9870.99, equity $9870.99
  open: none
c-reversal: 12 bars
  orders: submitted 5, filled 3, rejected 0, cancelled 1, pending 1
  trades: closed 1, wins 1, losses 0, break-even 0
  money: realized $33.00, unrealized $42.43, balance $10033.00, equity $10075.43
  open: short 9087 EURUSD from 1.10437, marked 1.09970
d-short-stop: 7 bars
  orders: submitted 2, filled 2, rejected 0, cancelled 0, pending 0
  trades: closed 1, wins 0, losses 1, break-even 0
  money: realized -$101.82, unrealized $0.00, balance $9898.18, equity $9898.18
  open: none
`

func TestRun_PrintsTheApprovedSummaryOfEveryScenario(t *testing.T) {
	logs := captureLogs(t)

	var out bytes.Buffer
	require.NoError(t, run(&out))

	assert.Equal(t, approvedSummary, out.String())
	for _, scenario := range scenarios {
		assert.Contains(t, logs.String(), "scenario="+scenario)
	}
	assert.Contains(t, logs.String(), "order rejected", "B2's close is an expected rejection")
	assert.NotContains(t, logs.String(), "level=ERROR")
}

func TestRun_RunsTheScenariosTheTestsApprove(t *testing.T) {
	var names []string
	for _, s := range emaxScenarios {
		names = append(names, s.name)
	}
	assert.Equal(t, names, scenarios)
}

func TestRun_PrintsTheSameBytesEveryTime(t *testing.T) {
	captureLogs(t)

	var first, second bytes.Buffer
	require.NoError(t, run(&first))
	require.NoError(t, run(&second))

	assert.Equal(t, first.String(), second.String())
}

func TestRun_ReportsAMissingScenario(t *testing.T) {
	captureLogs(t)
	t.Chdir(t.TempDir())

	var out bytes.Buffer
	err := run(&out)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scenario a-reversal-exit")
	assert.Empty(t, out.String())
}

func TestResult_ZeroPnLIsABreakEven(t *testing.T) {
	br := newBroker("sim")
	act := br.addAccount("a", usd(10000))
	act.trades = []trade{{pnl: 3}, {pnl: -1}, {pnl: 0}, {pnl: 0}} // the balance is left alone

	r := newResult("s", 0, &trader{broker: br, account: act})

	assert.Equal(t, 4, r.trades)
	assert.Equal(t, 1, r.wins)
	assert.Equal(t, 1, r.losses)
	assert.Equal(t, 2, r.breakEven)
	assert.Equal(t, money(2), r.realized, "the trades' sum, not the balance change")
}

// The one-cent run's two opens are refused at sizing and never reach the broker;
// its close is queued and then rejected. All three count as rejected, and only the
// close as submitted.
func TestResult_CountsRefusalsWithoutDuplication(t *testing.T) {
	refused := runEMAXWithBalance(t, "a-reversal-exit", 1)

	r := newResult("s", 10, refused.trader)

	assert.Equal(t, 3, r.rejected)
	assert.Equal(t, 1, r.submitted)
	assert.Zero(t, r.filled)
	assert.Zero(t, r.pending)
	assert.Zero(t, r.trades)
}

func TestResult_CountsOnlyItsAccountOrders(t *testing.T) {
	br := newBroker("sim")
	a := br.addAccount("a", usd(10000))
	br.addAccount("b", usd(20000))
	for _, id := range []string{"a", "b"} {
		br.completed = append(br.completed,
			&order{accountID: id, state: stateFilled},
			&order{accountID: id, state: stateRejected},
			&order{accountID: id, state: stateCancelled})
		for _, symbol := range []string{"EURUSD", "GBPUSD"} {
			br.orders[symbol] = append(br.orders[symbol],
				&order{accountID: id, instrument: symbol, state: stateSubmitted})
		}
	}
	tr := &trader{broker: br, account: a, rejected: []*order{{accountID: "a", state: stateRejected}}}

	r := newResult("a", 0, tr)

	assert.Equal(t, 5, r.submitted, "three completed and two pending for account a")
	assert.Equal(t, 1, r.filled)
	assert.Equal(t, 2, r.rejected, "one before submission and one at the broker")
	assert.Equal(t, 1, r.cancelled)
	assert.Equal(t, 2, r.pending)
	assert.Equal(t, usd(10000), r.balance)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestResult_WriteReturnsTheWriterError(t *testing.T) {
	assert.EqualError(t, (&result{}).write(failingWriter{}), "disk full")
}

func TestFormatMoney(t *testing.T) {
	for want, m := range map[string]money{
		"$0.00":     0,
		"$0.05":     5,
		"-$0.05":    -5,
		"$10033.00": 1003300,
		"-$102.09":  -10209,
	} {
		assert.Equal(t, want, formatMoney(m))
	}
}

func TestFormatPrice(t *testing.T) {
	assert.Equal(t, "1.10437", formatPrice(110437))
	assert.Equal(t, "0.00005", formatPrice(5))
}
