package trader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// line is a journal line as a file reader sees it: nothing but its text.
type line map[string]any

func (l line) str(k string) string { s, _ := l[k].(string); return s }

func readJournal(t *testing.T, b *bytes.Buffer) []line {
	t.Helper()
	var out []line
	for _, text := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if text == "" {
			continue
		}
		var l line
		require.NoError(t, json.Unmarshal([]byte(text), &l), text)
		out = append(out, l)
	}
	return out
}

// byOrder groups the journal's states by order ordinal.
func byOrder(lines []line) map[int][]line {
	out := map[int][]line{}
	for _, l := range lines {
		n := int(l["order"].(float64))
		out[n] = append(out[n], l)
	}
	return out
}

func journaled(t *testing.T, scenario string) ([]line, *Result) {
	t.Helper()
	captureLogs(t)
	var buf bytes.Buffer
	res, err := RunSeriesWith(context.Background(), RunOptions{Journal: &buf}, scenario, time.Time{}, scenarioPrices(t, scenario))
	require.NoError(t, err)
	return readJournal(t, &buf), res
}

func TestRunSeriesWith_JournalsEveryTransitionInOrder(t *testing.T) {
	lines, _ := journaled(t, "a-reversal-exit")
	require.NotEmpty(t, lines)

	for i, l := range lines {
		assert.EqualValues(t, i+1, l["seq"], "seq counts the lines")
	}
	for n, ls := range byOrder(lines) {
		assert.Equal(t, "requested", ls[0].str("state"), "order %d: the first line of an order is what was asked, so none was lost", n)
		assert.NotEmpty(t, ls[0].str("instrument"))
		assert.NotEmpty(t, ls[0].str("action"))
		assert.NotEmpty(t, ls[0].str("cause"))
	}

	// The strategy's first order is order 1: an open EMAX sized and the broker queued, then filled.
	var states []string
	for _, l := range byOrder(lines)[1] {
		states = append(states, l.str("state"))
	}
	assert.Equal(t, []string{"requested", "sized", "submitted", "filled"}, states)
	first := byOrder(lines)[1]
	assert.Equal(t, "open", first[0].str("action"))
	assert.Equal(t, "long", first[0].str("direction"))
	assert.Equal(t, "market", first[0].str("type"))
	assert.Equal(t, "strategy", first[0].str("cause"))
	assert.Empty(t, first[0].str("id"), "no id before the broker queues it")
	assert.NotEmpty(t, first[1].str("units"), "the account's size, on the sized line")
	assert.NotEmpty(t, first[2].str("id"), "the broker's id once queued")
	assert.NotEmpty(t, first[3].str("fill_price"))
	assert.Equal(t, "open", first[3].str("action"), "a fill line says what it is")
}

func TestRunSeriesWith_JournalsAProtectiveStopExit(t *testing.T) {
	lines, _ := journaled(t, "b2-stop-cross")

	var stopFills []line
	for _, l := range lines {
		if l.str("state") == "filled" && l.str("cause") == "stop loss" {
			stopFills = append(stopFills, l)
		}
	}
	require.Len(t, stopFills, 1, "the broker's stop filled once")
	f := stopFills[0]
	assert.Equal(t, "close", f.str("action"))
	assert.Equal(t, "long", f.str("direction"))
	assert.NotEmpty(t, f.str("id"))
	assert.NotEmpty(t, f.str("fill_price"))
	assert.NotEmpty(t, f.str("units"))

	// The stop was made by the broker when the open filled, and its requested line is in the journal.
	stop := byOrder(lines)[int(f["order"].(float64))]
	assert.Equal(t, "requested", stop[0].str("state"))
	assert.Equal(t, "stop", stop[0].str("type"))
	assert.NotEmpty(t, stop[0].str("price"), "where the stop was")
}

func TestRunSeriesWith_JournalingChangesNothingTheRunTrades(t *testing.T) {
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			captureLogs(t)
			prices := scenarioPrices(t, scenario)
			plain, err := RunSeries(context.Background(), scenario, time.Time{}, prices)
			require.NoError(t, err)

			var buf bytes.Buffer
			with, err := RunSeriesWith(context.Background(), RunOptions{Journal: &buf}, scenario, time.Time{}, prices)
			require.NoError(t, err)

			var a, b bytes.Buffer
			require.NoError(t, plain.Write(&a))
			require.NoError(t, with.Write(&b))
			assert.Equal(t, a.String(), b.String())
			assert.Equal(t, plain.Summary(), with.Summary())
			assert.NotZero(t, buf.Len())
		})
	}
}

func TestRunSeriesWith_TheSameRunWritesTheSameJournal(t *testing.T) {
	a, _ := journaled(t, "c-reversal")
	b, _ := journaled(t, "c-reversal")
	assert.Equal(t, a, b, "simulated time only, no wall clock, no run-varying value")
}

// failAfterWriter accepts n writes and then fails.
type failAfterWriter struct {
	n      int
	writes int
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes > w.n {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}

func TestRunSeriesWith_AJournalThatCannotWriteEndsTheRunWithItsError(t *testing.T) {
	captureLogs(t)
	prices := scenarioPrices(t, "a-reversal-exit")

	w := &failAfterWriter{n: 3}
	res, err := RunSeriesWith(context.Background(), RunOptions{Journal: w}, "x", time.Time{}, prices)
	require.ErrorContains(t, err, "journal: disk full")
	assert.Nil(t, res)
	assert.Equal(t, 4, w.writes, "after the first failure it stops writing")
}

// shortWriter writes one byte fewer than it is given and reports no error, which
// io.Writer forbids but nothing stops a writer doing.
type shortWriter struct{ buf bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	n, _ := w.buf.Write(p[:len(p)-1])
	return n, nil
}

func TestRunSeriesWith_AShortWriteIsAFailedJournalNotASuccess(t *testing.T) {
	captureLogs(t)
	w := &shortWriter{}

	res, err := RunSeriesWith(context.Background(), RunOptions{Journal: w}, "x", time.Time{}, scenarioPrices(t, "a-reversal-exit"))

	require.ErrorIs(t, err, io.ErrShortWrite)
	require.ErrorContains(t, err, "journal")
	assert.Nil(t, res)
	assert.NotContains(t, w.buf.String(), "\n", "no line was reported written, and none is complete")
}

func TestJournal_ErrOfNilIsNil(t *testing.T) {
	var j *journal
	assert.NoError(t, j.err())
}

func TestJournal_FinalFrameOrdersAreKeptEvenThoughNothingFollows(t *testing.T) {
	captureLogs(t)
	prices := scenarioPrices(t, remoteScenario)
	last := len(prices.Bars) // one group per bar

	s := &feedbackScript{orders: map[int]func(marketdata.Bar) *order{last: entryOrder(108700, 0)}}
	tr := newRemoteTraderWith(t, s, usd(1000))
	var buf bytes.Buffer
	tr.broker.journal = newJournal(&buf)
	require.NoError(t, (&backtest{}).runSeries(context.Background(), tr, prices))

	lines := readJournal(t, &buf)
	require.Equal(t, 3, len(lines), "requested, sized and submitted on the last frame: the broker never saw another bar")
	for i, want := range []string{"requested", "sized", "submitted"} {
		assert.Equal(t, want, lines[i].str("state"))
	}
	assert.Empty(t, s.told[last-1], "and no OnBars could have told the strategy")
}

func TestJournal_ARefusalIsRequestedThenRejectedAndNeverHasAnID(t *testing.T) {
	captureLogs(t)
	s := &feedbackScript{orders: map[int]func(marketdata.Bar) *order{3: entryOrder(108700, 0)}}
	tr := newRemoteTraderWith(t, s, usd(0))
	var buf bytes.Buffer
	tr.broker.journal = newJournal(&buf)
	require.NoError(t, (&backtest{}).runSeries(context.Background(), tr, scenarioPrices(t, remoteScenario)))

	lines := readJournal(t, &buf)
	require.Len(t, lines, 2)
	assert.Equal(t, "requested", lines[0].str("state"))
	assert.Equal(t, "rejected", lines[1].str("state"))
	assert.NotEmpty(t, lines[1].str("reason"))
	assert.Empty(t, lines[1].str("id"))
}

func TestJournal_ATargetAddedByAnAmendIsJournaledFromItsFirstLine(t *testing.T) {
	captureLogs(t)
	amendTarget := func(b marketdata.Bar) *order {
		return &order{instrument: b.Instrument, action: amend, orderType: marketOrder, takeProfit: px(115000), Time: b.Time}
	}
	s := &feedbackScript{orders: map[int]func(marketdata.Bar) *order{
		3: entryOrder(108700, 0), // opens with a stop and no target
		6: amendTarget,
	}}
	tr := newRemoteTraderWith(t, s, usd(1000))
	var buf bytes.Buffer
	tr.broker.journal = newJournal(&buf)
	require.NoError(t, (&backtest{}).runSeries(context.Background(), tr, scenarioPrices(t, remoteScenario)))

	var target line
	for _, ls := range byOrder(readJournal(t, &buf)) {
		if ls[0].str("cause") == "take profit" {
			target = ls[0]
		}
	}
	require.NotNil(t, target, "the broker's new target order has a journal line")
	assert.Equal(t, "requested", target.str("state"), "its first line, not a later one")
	assert.Equal(t, "limit", target.str("type"))
}

// riskRun is a trader on a 10,000 USD account whose orders are journaled.
func riskRun(t *testing.T) (*trader, *bytes.Buffer) {
	t.Helper()
	captureLogs(t)
	br, act := newFillBroker()
	var buf bytes.Buffer
	br.journal = newJournal(&buf)
	return &trader{broker: br, account: act}, &buf
}

// riskEntry is a EURUSD open from 1.10000 decided on day 0.
func riskEntry(d direction, stop int64) *order {
	return &order{Time: testStart, instrument: "EURUSD", action: open, direction: d, orderType: marketOrder,
		price: eurusd1_10, stopLoss: px(stop)}
}

// amount is the USD amount of a journal line's money field, or "" when the field
// is absent.
func (l line) amount(t *testing.T, k string) string {
	t.Helper()
	v, ok := l[k]
	if !ok {
		return ""
	}
	m, ok := v.(map[string]any)
	require.True(t, ok, "%s is money: %v", k, v)
	require.Equal(t, "USD", m["currency"])
	s, ok := m["amount"].(string)
	require.True(t, ok, "%s amount is a decimal string", k)
	return s
}

// only is the one line that match accepts.
func only(t *testing.T, lines []line, match func(line) bool) line {
	t.Helper()
	var found []line
	for _, l := range lines {
		if match(l) {
			found = append(found, l)
		}
	}
	require.Len(t, found, 1)
	return found[0]
}

// riskKeys are the journal's risk record fields.
var riskKeys = []string{"binding_limit", "planned_risk", "risk_at_fill", "open_order", "excess_loss"}

// assertRiskKeys asserts that of the risk record l has exactly the fields named.
func assertRiskKeys(t *testing.T, l line, want ...string) {
	t.Helper()
	var got []string
	for _, k := range riskKeys {
		if _, ok := l[k]; ok {
			got = append(got, k)
		}
	}
	assert.ElementsMatch(t, want, got, "%s %s %s", l.str("state"), l.str("action"), l.str("cause"))
}

func TestJournal_RecordsRiskFromSizingToExit(t *testing.T) {
	flat := riskBar(1, 110000, 110000, 110000, 110000)
	for _, tc := range []struct {
		name   string
		dir    direction
		stop   int64
		bars   []marketdata.Bar
		atFill string
		excess string // "" when the exit lost no more than planned
	}{
		{"a stop at its price loses what was planned", long, 109450,
			[]marketdata.Bar{flat, riskBar(2, 110000, 110000, 109400, 109450)}, "99.9955", ""},
		{"long, adverse entry fill", long, 109450,
			[]marketdata.Bar{riskBar(1, 110550, 110600, 110500, 110550), riskBar(2, 110500, 110500, 109400, 109450)}, "199.991", "99.9955"},
		{"short, adverse entry fill", short, 110550,
			[]marketdata.Bar{riskBar(1, 109450, 109500, 109400, 109450), riskBar(2, 109500, 110600, 109500, 110550)}, "199.991", "99.9955"},
		{"long, favourable entry fill", long, 109450,
			[]marketdata.Bar{riskBar(1, 109725, 109725, 109700, 109725), riskBar(2, 109700, 109700, 109400, 109450)}, "49.99775", ""},
		{"long, gap through the stop", long, 109450,
			[]marketdata.Bar{flat, riskBar(2, 108900, 108900, 108900, 108900)}, "99.9955", "99.9955"},
		{"short, gap through the stop", short, 110550,
			[]marketdata.Bar{flat, riskBar(2, 111100, 111100, 111100, 111100)}, "99.9955", "99.9955"},
		// The stop is due at once and fills at the same open: nothing is lost.
		{"long, filled beyond the stop", long, 109450,
			[]marketdata.Bar{riskBar(1, 109400, 109400, 109400, 109400)}, "0", ""},
		{"short, filled beyond the stop", short, 110550,
			[]marketdata.Bar{riskBar(1, 110600, 110600, 110600, 110600)}, "0", ""},
		{"long, filled at the stop", long, 109450,
			[]marketdata.Bar{riskBar(1, 109450, 109450, 109450, 109450)}, "0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, buf := riskRun(t)
			ctx := context.Background()
			o := riskEntry(tc.dir, tc.stop)
			require.NoError(t, tr.handleOrder(ctx, o))
			for _, b := range tc.bars {
				require.NoError(t, tr.broker.onBar(ctx, b))
			}
			require.Empty(t, tr.account.positions)

			lines := readJournal(t, buf)
			entry := byOrder(lines)[1]
			require.Len(t, entry, 4)
			assertRiskKeys(t, entry[0])
			assertRiskKeys(t, entry[1], "binding_limit", "planned_risk")
			assertRiskKeys(t, entry[2])
			assertRiskKeys(t, entry[3], "risk_at_fill")
			assert.Equal(t, "risk", entry[1].str("binding_limit"))
			assert.Equal(t, "99.9955", entry[1].amount(t, "planned_risk"))
			assert.Equal(t, tc.atFill, entry[3].amount(t, "risk_at_fill"))

			exit := only(t, lines, func(l line) bool { return l.str("state") == "filled" && l.str("action") == "close" })
			if tc.excess == "" {
				assertRiskKeys(t, exit, "open_order")
			} else {
				assertRiskKeys(t, exit, "open_order", "excess_loss")
				assert.Equal(t, tc.excess, exit.amount(t, "excess_loss"))
			}
			assert.Equal(t, o.id, exit.str("open_order"), "the exit names the open, and so its sized line")
			assert.Equal(t, o.id, entry[3].str("id"))
		})
	}
}

// The record is of the sizing: a stop that trailed or was amended since changes
// neither the planned risk nor what an exit is compared with.
func TestJournal_ExcessLossIsAgainstThePlanNotAMovedStop(t *testing.T) {
	for _, tc := range []struct {
		name   string
		trail  int64 // points; 0: the stop is amended to 1.09900 instead
		gap    int64 // the open that gaps through the moved stop
		excess string
	}{
		{"trailed, lost less than planned", 500, 109750, ""},
		{"trailed, lost more than planned", 500, 109300, "27.2715"},
		{"amended, lost less than planned", 0, 109750, ""},
		{"amended, lost more than planned", 0, 109300, "27.2715"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, buf := riskRun(t)
			ctx := context.Background()
			o := riskEntry(long, 109450)
			if tc.trail != 0 {
				o.trail = px(tc.trail)
			}
			require.NoError(t, tr.handleOrder(ctx, o))
			// A high of 1.10300 trails the stop up to 1.09800.
			require.NoError(t, tr.broker.onBar(ctx, riskBar(1, 110000, 110300, 109900, 110000)))
			if tc.trail == 0 {
				require.NoError(t, tr.handleOrder(ctx, &order{Time: testStart.AddDate(0, 0, 1), instrument: "EURUSD",
					action: amend, orderType: marketOrder, stopLoss: px(109900)}))
			}
			require.NoError(t, tr.broker.onBar(ctx, riskBar(2, tc.gap, tc.gap, tc.gap, tc.gap)))
			require.Empty(t, tr.account.positions)

			lines := readJournal(t, buf)
			assert.Equal(t, "99.9955", byOrder(lines)[1][3].amount(t, "risk_at_fill"), "from the stop the open carried")
			exit := only(t, lines, func(l line) bool { return l.str("state") == "filled" && l.str("action") == "close" })
			assert.Equal(t, tc.excess, exit.amount(t, "excess_loss"))
			for _, l := range lines {
				closeFill := l.str("state") == "filled" && l.str("action") == "close"
				if l["order"].(float64) != 1 && !closeFill {
					assertRiskKeys(t, l) // exits, amends and their moves carry no record
				}
			}
		})
	}
}

func TestJournal_ExcessLossOfAStrategyCloseAndAMarginCloseout(t *testing.T) {
	// Filled 0.5% adversely, then closed at 1.09500 before the stop triggers.
	entry := riskBar(1, 110550, 110600, 110500, 110550)
	t.Run("strategy close", func(t *testing.T) {
		tr, buf := riskRun(t)
		ctx := context.Background()
		require.NoError(t, tr.handleOrder(ctx, riskEntry(long, 109450)))
		require.NoError(t, tr.broker.onBar(ctx, entry))
		require.NoError(t, tr.handleOrder(ctx, &order{Time: entry.Time, instrument: "EURUSD", action: close, orderType: marketOrder}))
		require.NoError(t, tr.broker.onBar(ctx, riskBar(2, 109500, 109500, 109500, 109500)))

		exit := only(t, readJournal(t, buf), func(l line) bool {
			return l.str("state") == "filled" && l.str("action") == "close"
		})
		assert.Equal(t, "strategy", exit.str("cause"))
		assert.Equal(t, "90.905", exit.amount(t, "excess_loss"))
	})
	t.Run("margin closeout", func(t *testing.T) {
		tr, buf := riskRun(t)
		tr.broker.margin = func(a *account) (marginStatus, error) {
			if len(a.positions) > 0 {
				return marginLiquidate, nil
			}
			return marginHealthy, nil
		}
		ctx := context.Background()
		o := riskEntry(long, 109450)
		require.NoError(t, tr.handleOrder(ctx, o))
		require.NoError(t, tr.broker.onBar(ctx, riskBar(1, 110550, 110550, 109500, 109500)))

		exit := only(t, readJournal(t, buf), func(l line) bool {
			return l.str("state") == "filled" && l.str("action") == "close"
		})
		assert.Equal(t, "margin closeout", exit.str("cause"))
		assert.Equal(t, o.id, exit.str("open_order"))
		assert.Equal(t, "90.905", exit.amount(t, "excess_loss"))
	})
}

func TestJournal_RiskRecordOfARefusedOpen(t *testing.T) {
	t.Run("below the minimum at sizing", func(t *testing.T) {
		tr, buf := riskRun(t)
		tr.account.balance = num.MustParseMoney("0.001", usdCurrency)
		require.NoError(t, tr.handleOrder(context.Background(), riskEntry(long, 109450)))

		refused := only(t, readJournal(t, buf), func(l line) bool { return l.str("state") == "rejected" })
		assertRiskKeys(t, refused, "binding_limit")
		assert.Equal(t, "risk", refused.str("binding_limit"))
	})
	t.Run("no margin left", func(t *testing.T) {
		tr, buf := riskRun(t)
		tr.account.balance = usd(0)
		require.NoError(t, tr.handleOrder(context.Background(), riskEntry(long, 109450)))

		refused := only(t, readJournal(t, buf), func(l line) bool { return l.str("state") == "rejected" })
		assertRiskKeys(t, refused, "binding_limit")
		assert.Equal(t, "margin", refused.str("binding_limit"))
	})
	t.Run("at the fill", func(t *testing.T) {
		tr, buf := riskRun(t)
		ctx := context.Background()
		require.NoError(t, tr.handleOrder(ctx, riskEntry(long, 109989))) // margin capped
		require.NoError(t, tr.broker.onBar(ctx, riskBar(1, 110001, 110001, 110001, 110001)))

		lines := readJournal(t, buf)
		refused := only(t, lines, func(l line) bool { return l.str("state") == "rejected" })
		assertRiskKeys(t, refused, "binding_limit")
		assert.Equal(t, "margin", refused.str("binding_limit"))
		assert.Equal(t, "49.99995", byOrder(lines)[1][1].amount(t, "planned_risk"))
	})
}

func TestKnownMoney(t *testing.T) {
	assert.Nil(t, knownMoney(num.Money{}), "unset")
	zero := usd(0)
	assert.Equal(t, &zero, knownMoney(zero), "zero is known")
}
