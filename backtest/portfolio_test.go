package backtest_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	"github.com/rustyeddy/trader/strategy"
)

// newBindingRiskRunnerParams builds a RunnerParams shaped like
// runner_test.go's own mustRunnerParams, except its Pipeline is
// configured with a real, *binding* risk.MaxOpenPositionsRule(max)
// instead of a zero-rule Engine, and its Journal is rec, so the
// resulting per-instrument decisions can be inspected directly (issue
// #224 review, point 1: #223's own MaxOpenPositionsRule(5) never
// actually bound with only two instruments, so it could not by itself
// prove shared risk state crosses instrument boundaries — max=1 with
// two simultaneous entries does).
func newBindingRiskRunnerParams(t *testing.T, max int, strat strategy.Strategy, rec journal.Recorder) backtest.RunnerParams {
	t.Helper()

	mgr := newSchedulerTestManager(t)
	publishBothInstrumentsFixture(t, mgr)
	resolver := instrumentResolverFor(t)
	h := newSchedulerHarness(t, schedulerSpan(t).Start())

	planner, err := execution.NewPlanner(execution.Deps{Clock: h.clockObj, IDs: h.ids})
	require.NoError(t, err)
	rule, err := risk.NewMaxOpenPositionsRule(max)
	require.NoError(t, err)
	engine, err := risk.NewEngine(rule)
	require.NoError(t, err)
	pl, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  h.broker,
		IDs:     h.ids,
	})
	require.NoError(t, err)

	acc, err := h.broker.OpenAccount(context.Background(), h.accountID)
	require.NoError(t, err)

	fill, slippage, commission := mustRunnerModels(t)
	ruleInfo, err := backtest.NewComponentInfo("max_open_positions", "v1", map[string]string{"max": fmt.Sprintf("%d", max)})
	require.NoError(t, err)

	return backtest.RunnerParams{
		Manager:         mgr,
		Resolver:        resolver,
		Clock:           h.clockObj,
		IDs:             h.ids,
		Pipeline:        pl,
		Account:         acc,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("0.01000"),
		RiskRules:       []backtest.ComponentInfo{ruleInfo},
		FillModel:       fill,
		SlippageModel:   slippage,
		CommissionModel: commission,
		Strategy:        strat,
		Span:            schedulerSpan(t),
		Journal:         rec,
	}
}

// TestBacktest_SharedRiskBindsAcrossInstruments is #224's own required
// binding-risk regression (issue #224 review, point 1): a
// risk.MaxOpenPositionsRule(1) engine, one shared Account/Pipeline,
// and a strategy (mustEnterOnFirstBarStrategy, from scheduler_test.go)
// that enters both EUR/USD and GBP/USD on their shared first bar —
// the same timestamp, per Scheduler's own documented same-timestamp
// pairing. Both intents are evaluated against the same frozen T-batch
// account state, then submitted in Scheduler's own canonical
// deterministic order; the first to submit opens the account's one
// permitted position, and the second is rejected because the shared
// account now already holds it. This is the "second instrument's
// outcome depends on state created by the first" property #223's own
// MaxOpenPositionsRule(5) (which never bound) could not demonstrate,
// proving all of: one Scheduler, one Account, shared risk state,
// same-timestamp deterministic submission order, and no per-symbol
// engine fork.
func TestBacktest_SharedRiskBindsAcrossInstruments(t *testing.T) {
	rec := &capturingRecorder{}
	strat := mustEnterOnFirstBarStrategy(t)
	params := newBindingRiskRunnerParams(t, 1, strat, rec)

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)
	result, err := runner.Run(context.Background())
	require.NoError(t, err)

	// Exactly one position across the whole shared account — not one
	// per instrument — is the headline economic assertion: the rule
	// actually bound.
	require.Len(t, result.Account.Positions(), 1,
		"risk.MaxOpenPositionsRule(1) must have allowed exactly one of the two simultaneous entries across the shared account")

	// Pin the winner, not just "exactly one won" (issue #242 review):
	// Scheduler's own canonical same-timestamp tie-break is
	// deterministic, so which instrument's intent is submitted first
	// (and therefore wins the account's one permitted position) is not
	// arbitrary — it must always be EUR/USD, confirmed empirically
	// stable across five repeated runs. Pinning it here means this
	// test itself protects that tie-break rule, rather than only
	// observing "deterministic" outside the assertion.
	assert.True(t, result.Account.Positions()[0].Listing.InstrumentID().Equal(eurusdID(t)),
		"expected EUR/USD to deterministically win Scheduler's same-timestamp tie-break, got %s", result.Account.Positions()[0].Listing.InstrumentID())

	decisions := decisionsFromJournal(t, rec.all())
	require.Len(t, decisions, 2, "both instruments' Enter intents must have reached a risk decision")

	allowed, rejected := 0, 0
	for _, d := range decisions {
		if d.Allowed {
			allowed++
		} else {
			rejected++
			assert.NotEmpty(t, d.Violations, "a rejected decision must name the violation that rejected it")
			foundRule := false
			for _, v := range d.Violations {
				if v.Rule == "max_open_positions" {
					foundRule = true
				}
			}
			assert.True(t, foundRule, "the rejected decision's own violation must name max_open_positions, not some other cause: %+v", d.Violations)
		}
	}
	assert.Equal(t, 1, allowed, "exactly one of the two simultaneous entries must have been allowed")
	assert.Equal(t, 1, rejected, "exactly one of the two simultaneous entries must have been rejected by the shared account's own risk state")
}

// decisionsFromJournal extracts every KindDecision payload from
// records, in journal order.
func decisionsFromJournal(t *testing.T, records []journal.Record) []risk.Decision {
	t.Helper()
	var out []risk.Decision
	for _, rec := range records {
		if rec.Kind == journal.KindDecision {
			out = append(out, *rec.Decision)
		}
	}
	return out
}
