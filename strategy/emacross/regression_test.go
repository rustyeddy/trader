// This file is issue #255 (EMA-10)'s own required horizontal-slice
// regression: one deterministic test proving the entire path —
// canonical bars -> EMA calculation -> crossover detection ->
// order.Intent -> M4 execution/risk -> simulated fill -> journal ->
// trades/account -> metrics/report — works together, through the real
// public composition path (service/backtest.Service, the same seam
// EMA-05/EMA-06 already exercise), not a mock that bypasses M3/M4/M5.
package emacross_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/report"
	"github.com/rustyeddy/trader/strategy/emacross"
)

// TestEmacross_EndToEndRegression replays the same fixture EMA-05/
// EMA-06/EMA-08's own tests already prove individual stages of, and
// additionally asserts two things none of them do in one place: the
// journal's exact, fully-ordered Kind sequence for the whole run (not
// merely per-Kind counts/presence), and that projecting the resulting
// RunResponse through report.NewBacktestReport — the same final
// consumer a real CLI run feeds — reconciles with the same
// trade/account figures already asserted directly.
func TestEmacross_EndToEndRegression(t *testing.T) {
	resp, rec := runEMACrossoverFixture(t)

	// The fixture's own known, fixed instants: the bar-6 crossover
	// detection instant (the bar whose close makes EMA(3) cross
	// EMA(5)), its bar-7 next-bar-open fill instant, the bar-11
	// reversal detection instant, and its bar-12 next-bar-open fill
	// instant. These are the same instants
	// TestEmacross_EntryExitReversalThroughRealPipeline already names
	// as bar7/bar8/bar12/bar13 open by clock hour; named here again so
	// this test is a complete, self-contained regression that does not
	// depend on another test file continuing to exist unchanged.
	bar6Close := time.Date(2024, time.March, 4, 6, 0, 0, 0, time.UTC)
	bar7Fill := time.Date(2024, time.March, 4, 7, 0, 0, 0, time.UTC)
	bar11Close := time.Date(2024, time.March, 4, 11, 0, 0, 0, time.UTC)
	bar12Fill := time.Date(2024, time.March, 4, 12, 0, 0, 0, time.UTC)

	usd := num.MustParseCurrency("USD")

	// Causal path proof: the exact bar-7 bullish cross and bar-12
	// bearish reversal, their fills at the fixture's own known next-
	// bar-open prices, and the resulting trade/account state — the
	// same assertions TestEmacross_EntryExitReversalThroughRealPipeline
	// already makes, now pinned to exact fixed values (not merely a
	// negative-PnL/side shape) per PR #266 review.
	require.Len(t, resp.Trades, 1, "the bar-12 reversal must have closed the bar-7 long as one realized trade")
	closed := resp.Trades[0]
	assert.Equal(t, order.Long, closed.Side)
	assert.True(t, closed.OpenedAt.Equal(bar7Fill), "the long must open at bar 7's own next-bar-open time, got %s", closed.OpenedAt)
	assert.True(t, closed.ClosedAt.Equal(bar12Fill), "the long must close at bar 12's own next-bar-open time, got %s", closed.ClosedAt)
	assert.Len(t, closed.EntryFillIDs, 1, "one entry fill: the bar-7 crossover's single Enter")
	assert.Len(t, closed.ExitFillIDs, 1, "one exit fill: the bar-12 reversal's Exit")
	assert.True(t, closed.RealizedPnL.Equal(num.MustParseMoney("-4", usd)),
		"the long entered at 1.10040 and exited at 1.10000 on 9996 units: a fixed, known loss, got %s", closed.RealizedPnL)

	require.Len(t, resp.OpenTrades, 1)
	open := resp.OpenTrades[0]
	assert.Equal(t, order.Short, open.Side)
	assert.True(t, open.OpenedAt.Equal(bar12Fill), "the re-entry must open at bar 12's own next-bar-open time, got %s", open.OpenedAt)
	assert.Len(t, open.EntryFillIDs, 1, "one entry fill: the bar-12 reversal's Enter")

	require.Len(t, resp.Account.Positions(), 1)
	position := resp.Account.Positions()[0]
	assert.Equal(t, order.Short, position.Side)
	assert.True(t, position.Quantity.Equal(num.MustParseQuantity("9996")), "got quantity %s", position.Quantity)
	require.NotNil(t, position.AvgPrice)
	assert.True(t, position.AvgPrice.Equal(num.MustParsePrice("1.10000")), "got avg price %s", position.AvgPrice)
	assert.True(t, resp.Account.Equity().Equal(num.MustParseMoney("10005.996", usd)), "got equity %s", resp.Account.Equity())

	// Decision-evidence proof (EMA-08/ADR-044): the exact Signal
	// payloads for both crossovers, correlated with the exact Intents
	// they produced — not merely "a Signal exists" and "an Intent
	// exists" independently.
	signals := rec.kinds(journal.KindSignal)
	require.Len(t, signals, 2)
	bullish, bearish := signals[0], signals[1]

	assert.True(t, bullish.Metadata.Timestamp.Equal(bar6Close), "got %s", bullish.Metadata.Timestamp)
	assert.Equal(t, map[string]string{
		"fast_ema":      "1.10025",
		"slow_ema":      "1.1002444444444448",
		"prev_relation": "below",
		"curr_relation": "above",
		"cross":         "bullish",
		"action":        "enter-long",
	}, bullish.Signal.Values)

	assert.True(t, bearish.Metadata.Timestamp.Equal(bar11Close), "got %s", bearish.Metadata.Timestamp)
	assert.Equal(t, map[string]string{
		"fast_ema":      "1.1003578125",
		"slow_ema":      "1.1004626428898034",
		"prev_relation": "above",
		"curr_relation": "below",
		"cross":         "bearish",
		"action":        "reverse",
	}, bearish.Signal.Values)

	intents := rec.kinds(journal.KindIntent)
	require.Len(t, intents, 3, "one Enter for the bar-6 cross, one Exit and one Enter for the bar-11 reversal")
	enterLong, exitLong, enterShort := intents[0], intents[1], intents[2]

	require.NotNil(t, enterLong.Intent)
	assert.Equal(t, order.IntentEnter, enterLong.Intent.Kind)
	assert.Equal(t, order.Buy, enterLong.Intent.Side)
	assert.Equal(t, bullish.Metadata.CorrelationID, enterLong.Metadata.CorrelationID,
		"the Enter intent must correlate with the Signal that produced it")

	require.NotNil(t, exitLong.Intent)
	assert.Equal(t, order.IntentExit, exitLong.Intent.Kind)
	require.NotNil(t, enterShort.Intent)
	assert.Equal(t, order.IntentEnter, enterShort.Intent.Kind)
	assert.Equal(t, order.Sell, enterShort.Intent.Side)
	assert.Equal(t, bearish.Metadata.CorrelationID, exitLong.Metadata.CorrelationID,
		"the reversal's Exit intent must correlate with the bearish Signal")
	assert.Equal(t, exitLong.Metadata.CorrelationID, enterShort.Metadata.CorrelationID,
		"the reversal's Exit and Enter intents must share one correlation, per ADR-005's correlated-pair intent style")

	// The exact, fully-ordered journal Kind sequence for the whole run
	// — issue #255's own "expected journal sequence ... asserted"
	// acceptance criterion. Built once, empirically, from a real run
	// (not hand-guessed), and pinned here so a change to causal
	// ordering anywhere in the path fails this test.
	//
	// Two things this sequence proves that were not obvious from the
	// individual per-stage tests EMA-05/06/08 already have:
	//
	//   - Signal precedes Intent for the same OnBar call. recordSignal
	//     runs synchronously inside OnBar before the intents are
	//     returned; Scheduler only journals a returned Intent after
	//     OnBar has already returned, so the Signal record for a given
	//     crossover always lands before that crossover's first Intent.
	//   - A correlated multi-intent OnBar result (bar 12's Exit+Enter
	//     reversal) records exactly one Signal, but each Intent is
	//     journaled and driven through its own full
	//     Proposal->Decision->Request->Order->Fill->Order cycle one at
	//     a time — the second intent's Intent record does not appear
	//     until the first intent's entire cycle has completed.
	//   - Each fill produces two KindOrder records (an initial
	//     working/accepted transition, then a second post-fill status
	//     transition) and no KindAccount record at all in this
	//     execution path.
	//   - journalTrades (backtest/runner.go) records every derived
	//     trade — closed and still-open — as its own KindTrade entry,
	//     so the run ends with two Trade records: the closed long, then
	//     the still-open short.
	wantKinds := []journal.Kind{
		journal.KindRunStarted,
		// Bar 7: bullish cross -> Enter(Buy) queued.
		journal.KindSignal,
		journal.KindIntent,
		// Bar 8 flush: bar-7's Enter fills.
		journal.KindProposal,
		journal.KindDecision,
		journal.KindRequest,
		journal.KindOrder,
		journal.KindFill,
		journal.KindOrder,
		// Bar 12: bearish cross -> Exit+Enter(Sell) queued (correlated),
		// one Signal shared by both.
		journal.KindSignal,
		journal.KindIntent, // Exit
		journal.KindProposal,
		journal.KindDecision,
		journal.KindRequest,
		journal.KindOrder,
		journal.KindFill,
		journal.KindOrder,
		journal.KindIntent, // Enter
		journal.KindProposal,
		journal.KindDecision,
		journal.KindRequest,
		journal.KindOrder,
		journal.KindFill,
		journal.KindOrder,
		journal.KindTrade, // closed: the exit closes the bar-7 long
		journal.KindTrade, // open: the new short from the reversal's Enter
		journal.KindRunCompleted,
	}
	gotKinds := make([]journal.Kind, len(rec.records))
	for i, r := range rec.records {
		gotKinds[i] = r.Kind
	}
	assert.Equal(t, wantKinds, gotKinds, "the journal's exact causal record order must match the real signal -> intent -> proposal -> decision -> request -> order -> fill -> order -> trade path")

	// Report/metrics projection: the same public consumer a real CLI
	// run feeds (report.NewBacktestReport), reconciling with the
	// trade/account figures already asserted above directly — proving
	// the metrics/report stage of the required path, not just backtest
	// package internals.
	rep := report.NewBacktestReport(report.BacktestInput{
		Manifest:    resp.Manifest,
		Account:     resp.Account,
		Trades:      resp.Trades,
		OpenTrades:  resp.OpenTrades,
		EquityCurve: resp.EquityCurve,
		Metrics:     resp.Metrics,
	})
	assert.Equal(t, emacross.Name, rep.Run.StrategyName)
	require.Equal(t, 1, rep.TradeStats.TradeCount)
	assert.Equal(t, 0, rep.TradeStats.Wins)
	assert.Equal(t, 1, rep.TradeStats.Losses, "the closed long realized a loss, per the fixture's own known prices")
	assert.True(t, rep.TradeStats.NetPnL.Equal(num.MustParseMoney("-4", usd)), "got %s", rep.TradeStats.NetPnL)
	require.Len(t, rep.BySide, 1, "only the closed long has a final win/loss outcome; the still-open short is excluded (backtest.Metrics' own documented scope)")
	assert.Equal(t, "long", rep.BySide[0].Side)
	assert.Equal(t, 1, rep.BySide[0].Count)
	assert.Equal(t, 0, rep.BySide[0].Wins)
	assert.Equal(t, 1, rep.BySide[0].Losses)
}

// TestEmacross_EndToEndRegression_Deterministic proves repeated runs of
// the identical fixture through the identical composition path produce
// identical observable results — issue #255's own "repeat the test to
// prove deterministic output" requirement — normalized against opaque
// run-local identifiers (RunID, event/correlation/order/fill ids)
// exactly the way backtest's own ADR-041 determinism suite already
// established for this same class of comparison.
func TestEmacross_EndToEndRegression_Deterministic(t *testing.T) {
	// signature captures the run's causal shape (kind + timestamp per
	// record, plus every Signal's own semantic payload) without opaque
	// run-local identifiers (RunID, event/correlation/order/fill IDs),
	// which are expected to differ run to run — the same normalization
	// backtest's own ADR-041 determinism suite established.
	signature := func(rec *memoryRecorder) string {
		var sb []byte
		for _, r := range rec.records {
			sb = fmt.Appendf(sb, "%s@%s", r.Kind, r.Metadata.Timestamp)
			if r.Kind == journal.KindSignal {
				sb = fmt.Appendf(sb, " strategy=%s fast_ema=%s slow_ema=%s prev_relation=%s curr_relation=%s cross=%s action=%s",
					r.Signal.Strategy,
					r.Signal.Values["fast_ema"], r.Signal.Values["slow_ema"],
					r.Signal.Values["prev_relation"], r.Signal.Values["curr_relation"],
					r.Signal.Values["cross"], r.Signal.Values["action"])
			}
			sb = append(sb, '\n')
		}
		return string(sb)
	}

	run := func() (string, string, string) {
		resp, rec := runEMACrossoverFixture(t)
		return signature(rec), resp.Manifest.ConfigDigest(), resp.Trades[0].RealizedPnL.String()
	}

	wantSignature, wantDigest, wantPnL := run()
	for i := range 3 {
		gotSignature, gotDigest, gotPnL := run()
		assert.Equal(t, wantSignature, gotSignature, "run %d: journal causal signature diverged", i+2)
		assert.Equal(t, wantDigest, gotDigest, "run %d: ConfigDigest diverged", i+2)
		assert.Equal(t, wantPnL, gotPnL, "run %d: realized PnL diverged", i+2)
	}
}
