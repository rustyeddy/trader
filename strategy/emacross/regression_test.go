// This file is issue #255 (EMA-10)'s own required horizontal-slice
// regression: one deterministic test proving the entire path —
// canonical bars -> EMA calculation -> crossover detection ->
// order.Intent -> M4 execution/risk -> simulated fill -> journal ->
// trades/account -> metrics/report — works together, through the real
// public composition path (svcbacktest.Service, the same seam EMA-05/
// EMA-06 already exercise), not a mock that bypasses M3/M4/M5.
package emacross_test

import (
	"testing"

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

	// Causal path proof: the exact bar-7 bullish cross and bar-12
	// bearish reversal, their fills at the fixture's own known next-
	// bar-open prices, and the resulting trade/account state — the
	// same assertions TestEmacross_EntryExitReversalThroughRealPipeline
	// already makes; repeated here so this one test is a complete,
	// self-contained regression that does not depend on another test
	// file continuing to exist unchanged.
	require.Len(t, resp.Trades, 1, "the bar-12 reversal must have closed the bar-7 long as one realized trade")
	closed := resp.Trades[0]
	assert.Equal(t, order.Long, closed.Side)
	sign, err := closed.RealizedPnL.Cmp(num.MustParseMoney("0", num.MustParseCurrency("USD")))
	require.NoError(t, err)
	assert.Negative(t, sign, "a completed round trip with realized PnL, per issue #255's own requirement")

	require.Len(t, resp.OpenTrades, 1)
	assert.Equal(t, order.Short, resp.OpenTrades[0].Side)
	require.Len(t, resp.Account.Positions(), 1)
	assert.Equal(t, order.Short, resp.Account.Positions()[0].Side)

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
	assert.Equal(t, wantKinds, gotKinds, "the journal's exact causal record order must match the real intent -> proposal -> decision -> request -> order -> fill -> account -> trade path")

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
	wantNetPnL, err := closed.RealizedPnL.Sub(closed.Costs)
	require.NoError(t, err)
	assert.True(t, rep.TradeStats.NetPnL.Equal(wantNetPnL))
	require.Len(t, rep.BySide, 1, "only the closed long has a final win/loss outcome; the still-open short is excluded (backtest.Metrics' own documented scope)")
	assert.Equal(t, "long", rep.BySide[0].Side)
}

// TestEmacross_EndToEndRegression_Deterministic proves repeated runs of
// the identical fixture through the identical composition path produce
// identical observable results — issue #255's own "repeat the test to
// prove deterministic output" requirement — normalized against opaque
// run-local identifiers (RunID, event/correlation/order/fill ids)
// exactly the way backtest's own ADR-041 determinism suite already
// established for this same class of comparison.
func TestEmacross_EndToEndRegression_Deterministic(t *testing.T) {
	run := func() (int, string, string) {
		resp, rec := runEMACrossoverFixture(t)
		var kinds []journal.Kind
		for _, r := range rec.records {
			kinds = append(kinds, r.Kind)
		}
		return len(kinds), resp.Manifest.ConfigDigest(), resp.Trades[0].RealizedPnL.String()
	}

	wantLen, wantDigest, wantPnL := run()
	for i := range 3 {
		gotLen, gotDigest, gotPnL := run()
		assert.Equal(t, wantLen, gotLen, "run %d: journal record count diverged", i+2)
		assert.Equal(t, wantDigest, gotDigest, "run %d: ConfigDigest diverged", i+2)
		assert.Equal(t, wantPnL, gotPnL, "run %d: realized PnL diverged", i+2)
	}
}
