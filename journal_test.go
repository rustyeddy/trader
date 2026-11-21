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
