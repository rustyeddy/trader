package execution

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	sim "github.com/rustyeddy/trader/adapters/broker/sim"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/clock"
	executionpkg "github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var vsTestStart = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func usd(s string) num.Money {
	return num.MustParseMoney(s, num.MustParseCurrency("USD"))
}

func mustGbpUsdListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "sim",
		Symbol:     "GBP_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func drainVSEvents(t *testing.T, reader brokerpkg.EventReader, n int) []brokerpkg.Event {
	t.Helper()
	ctx := context.Background()
	events := make([]brokerpkg.Event, 0, n)
	for range n {
		e, err := reader.Next(ctx)
		require.NoError(t, err)
		events = append(events, e)
	}
	return events
}

func assertNoMoreVSEventsSoon(t *testing.T, reader brokerpkg.EventReader) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := reader.Next(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// verticalSliceResult is every observable output
// runExecutionVerticalSliceScenario produces, gathered in one place so
// TestExecutionVerticalSlice_DeterministicAcrossRuns can diff two
// independent runs field by field.
type verticalSliceResult struct {
	FirstEvaluate    SubmitResponse
	Approved         SubmitResponse
	Rejected         SubmitResponse
	RejectedErr      error
	AccountBApprove  SubmitResponse
	SnapA, SnapB     account.Snapshot
	EventsA, EventsB []brokerpkg.Event
}

// runExecutionVerticalSliceScenario is issue #188's own primary
// deliverable: one deterministic, end-to-end scenario proving M4 as a
// complete capability integrated with the M3 simulator. It constructs
// the full stack via each lower package's own public constructors
// (sim.NewBroker, execution.NewPlanner, risk.NewEngine,
// pipeline.NewPipeline) — the same composition a real composition root
// performs — then exercises the scenario itself entirely through
// service/execution.Service and broker.Broker/Account, never reaching
// into any package's unexported internals, per this issue's own
// "exercise public/service boundaries rather than simulator internals"
// acceptance criterion.
//
// The scenario covers, on one shared deterministic simulated Broker
// and a risk.Engine composed of two real, configured M4 policies
// (PerTradeLossRule, MaxPositionQuantityRule — not a synthetic
// always-reject test double):
//
//   - Account A: a read-only Evaluate that never mutates the broker.
//   - Account A: an approved Enter intent, sized, planned,
//     risk-evaluated, submitted, and filled.
//   - Account A: a second, much larger Enter intent that real risk
//     policy rejects — proving no order/fill/event mutation occurs.
//   - Account B: an entirely independent approved scenario on a
//     different instrument, proving account isolation.
//
// Every input — Deps.Clock, Deps.IDs, prices, and the two configured
// risk.Rule thresholds — is fully deterministic (clock.NewSimulated,
// id.NewDeterministic, a fixed price map, literal Go values), so two
// calls to this function must produce byte-identical results; that
// equality is what TestExecutionVerticalSlice_DeterministicAcrossRuns
// actually asserts.
func runExecutionVerticalSliceScenario(t *testing.T) verticalSliceResult {
	t.Helper()
	ctx := context.Background()

	c := clock.NewSimulated(vsTestStart)
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))

	accountA, err := id.GenerateAccountID(ids)
	require.NoError(t, err)
	accountB, err := id.GenerateAccountID(ids)
	require.NoError(t, err)

	prices := fixedPriceSource{
		"EUR_USD": num.MustParsePrice("1.10000"),
		"GBP_USD": num.MustParsePrice("1.25000"),
	}
	b, err := sim.NewBroker("sim", sim.Deps{Clock: c, IDs: ids, Prices: prices},
		sim.AccountConfig{AccountID: accountA, StartingCash: usd("10000")},
		sim.AccountConfig{AccountID: accountB, StartingCash: usd("5000")},
	)
	require.NoError(t, err)

	planner, err := executionpkg.NewPlanner(executionpkg.Deps{Clock: c, IDs: ids})
	require.NoError(t, err)

	// Two real, configured M4 risk policies (#182, #183) — not a
	// synthetic always-reject test double — so the rejection this
	// scenario exercises demonstrates actual M4 risk admission, not
	// merely Engine's own plumbing.
	perTradeLoss, err := risk.NewPerTradeLossRule(num.MustParseRate("0.05")) // 5% of equity max planned loss
	require.NoError(t, err)
	maxQty, err := risk.NewMaxPositionQuantityRule(num.MustParseQuantity("50000"))
	require.NoError(t, err)
	engine, err := risk.NewEngine(perTradeLoss, maxQty)
	require.NoError(t, err)

	p, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  b,
		IDs:     ids,
	})
	require.NoError(t, err)

	svc, err := New(b, p, nil)
	require.NoError(t, err)

	eurUsd := mustEurUsdListing(t)
	gbpUsd := mustGbpUsdListing(t)

	buildIntent := func(instID instrument.ID) order.Intent {
		intentID, err := id.GenerateIntentID(ids)
		require.NoError(t, err)
		eventID, err := id.GenerateEventID(ids)
		require.NoError(t, err)
		corrID, err := id.GenerateCorrelationID(ids)
		require.NoError(t, err)
		in, err := order.NewIntent(order.Intent{
			IntentID:   intentID,
			Kind:       order.IntentEnter,
			Instrument: instID,
			Side:       order.Buy,
			Metadata:   id.Metadata{EventID: eventID, CorrelationID: corrID},
		})
		require.NoError(t, err)
		return in
	}

	adverse := num.MustParsePrice("0.01000")

	// Account A: a read-only Evaluate — 1% risk fraction, well within
	// both configured policies — must never mutate the broker.
	firstEvaluate, err := svc.Evaluate(ctx, SubmitRequest{
		AccountID:       accountA,
		Intent:          buildIntent(eurUsd.InstrumentID()),
		Listing:         eurUsd,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.NoError(t, err)
	require.True(t, firstEvaluate.Decision.Allowed)

	accA, err := b.OpenAccount(ctx, accountA)
	require.NoError(t, err)
	snapBeforeSubmit, err := accA.Snapshot(ctx)
	require.NoError(t, err)
	require.Empty(t, snapBeforeSubmit.Positions(), "Evaluate must never open a position")

	// Account A: the real, approved submission — same 1% risk
	// fraction, sized/planned/risk-evaluated/submitted/filled.
	approved, err := svc.Submit(ctx, SubmitRequest{
		AccountID:       accountA,
		Intent:          buildIntent(eurUsd.InstrumentID()),
		Listing:         eurUsd,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.NoError(t, err)
	require.True(t, approved.Decision.Allowed)

	snapAfterApproved, err := accA.Snapshot(ctx)
	require.NoError(t, err)

	// Account A: a 50% risk-fraction Enter intent, added to the
	// existing position, blows through MaxPositionQuantityRule's own
	// configured 50000-unit cap (and likely PerTradeLossRule's own 5%
	// budget too) — a real risk rejection, not a synthetic one.
	rejected, rejectedErr := svc.Submit(ctx, SubmitRequest{
		AccountID:       accountA,
		Intent:          buildIntent(eurUsd.InstrumentID()),
		Listing:         eurUsd,
		RiskFraction:    num.MustParseRate("0.5"),
		AdverseDistance: &adverse,
	})
	require.False(t, rejected.Decision.Allowed)

	snapA, err := accA.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, snapAfterApproved, snapA, "a rejected submission must leave account A's snapshot byte-identical")

	readerA, err := accA.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = readerA.Close() }()
	// market buy = Working + Fill + Filled = 3. The rejected submission
	// produces zero additional events (#188's own "risk rejection
	// produces no order/fill/event mutation" acceptance criterion).
	eventsA := drainVSEvents(t, readerA, 3)
	assertNoMoreVSEventsSoon(t, readerA)

	// Account B: an entirely independent approved scenario on a
	// different instrument. Proving isolation requires showing both
	// directions: B's own final state below is untouched by everything
	// already done on A (asserted in the "account B" subtest), and —
	// just as importantly — A's own state, re-read after B's submit,
	// must still be byte-identical to what it was before B ever
	// existed. A regression where submitting on B accidentally mutated
	// A's snapshot or appended to A's event stream would pass a
	// one-directional check but must fail this one (review feedback on
	// PR #205).
	accountBApprove, err := svc.Submit(ctx, SubmitRequest{
		AccountID:       accountB,
		Intent:          buildIntent(gbpUsd.InstrumentID()),
		Listing:         gbpUsd,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.NoError(t, err)
	require.True(t, accountBApprove.Decision.Allowed)

	accB, err := b.OpenAccount(ctx, accountB)
	require.NoError(t, err)
	snapB, err := accB.Snapshot(ctx)
	require.NoError(t, err)

	readerB, err := accB.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = readerB.Close() }()
	eventsB := drainVSEvents(t, readerB, 3)
	assertNoMoreVSEventsSoon(t, readerB)

	// Re-read account A after B's submission: its snapshot and its
	// complete event stream (read again from the beginning) must be
	// byte-identical to what was captured before B ever submitted.
	snapAAfterB, err := accA.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, snapA, snapAAfterB, "account B's submission must never mutate account A's snapshot")

	readerAAfterB, err := accA.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = readerAAfterB.Close() }()
	eventsAAfterB := drainVSEvents(t, readerAAfterB, 3)
	assertNoMoreVSEventsSoon(t, readerAAfterB)
	require.Equal(t, eventsA, eventsAAfterB, "account B's submission must never append to account A's event stream")

	return verticalSliceResult{
		FirstEvaluate:   firstEvaluate,
		Approved:        approved,
		Rejected:        rejected,
		RejectedErr:     rejectedErr,
		AccountBApprove: accountBApprove,
		SnapA:           snapA,
		SnapB:           snapB,
		EventsA:         eventsA,
		EventsB:         eventsB,
	}
}

// TestExecutionVerticalSlice_FullScenarioProducesExpectedState is the
// concrete, hand-computed proof behind
// runExecutionVerticalSliceScenario: every balance, position, and
// event count below is derived by hand from the scenario's own fixed
// prices and configured risk thresholds, so a real ordering, sizing,
// or risk-admission regression fails at a specific, named assertion
// rather than merely "the snapshot changed."
func TestExecutionVerticalSlice_FullScenarioProducesExpectedState(t *testing.T) {
	result := runExecutionVerticalSliceScenario(t)

	t.Run("account A: evaluate never mutates the broker", func(t *testing.T) {
		require.NotEqual(t, order.Request{}, result.FirstEvaluate.Request)
		require.Equal(t, order.Order{}, result.FirstEvaluate.Order, "Evaluate must never populate Order")
	})

	t.Run("account A: approved submission is sized, filled, and matches hand-computed accounting", func(t *testing.T) {
		// 1% of 10000 equity = 100 risk budget; 0.01000 adverse
		// distance * multiplier 1 = 0.01 loss per unit; 100/0.01 =
		// 10000 units, already a whole increment.
		require.True(t, result.Approved.Proposal.Quantity.Equal(num.MustParseQuantity("10000")),
			"sized quantity: %s", result.Approved.Proposal.Quantity)
		require.Equal(t, order.StatusFilled, result.Approved.Order.Status)

		require.Len(t, result.SnapA.Positions(), 1)
		pos := result.SnapA.Positions()[0]
		assert.Equal(t, order.Long, pos.Side)
		assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("10000")), "position quantity: %s", pos.Quantity)
		require.NotNil(t, pos.AvgPrice)
		assert.True(t, pos.AvgPrice.Equal(num.MustParsePrice("1.10000")), "avg price: %s", pos.AvgPrice)

		// A market fill carries no realized PnL/fees (no CommissionModel
		// configured): cash stays exactly the starting balance.
		assert.True(t, result.SnapA.RealizedPnL().Equal(usd("0")), "realized PnL: %s", result.SnapA.RealizedPnL())
		require.NotEmpty(t, result.SnapA.CashBalances(), "a regression here must fail this assertion, not panic on [0]")
		assert.True(t, result.SnapA.CashBalances()[0].Equal(usd("10000")), "cash: %s", result.SnapA.CashBalances()[0])
	})

	t.Run("account A: oversized second intent is rejected by real risk policy with no mutation", func(t *testing.T) {
		require.ErrorIs(t, result.RejectedErr, ErrRejected)
		require.False(t, result.Rejected.Decision.Allowed)
		require.NotEmpty(t, result.Rejected.Decision.Violations)
		var sawMaxQty bool
		for _, v := range result.Rejected.Decision.Violations {
			if v.Rule == "max_position_quantity" {
				sawMaxQty = true
			}
		}
		assert.True(t, sawMaxQty, "expected max_position_quantity among the violations: %+v", result.Rejected.Decision.Violations)
		assert.Equal(t, order.Request{}, result.Rejected.Request, "Request must never be populated on rejection")
		assert.Equal(t, order.Order{}, result.Rejected.Order, "Order must never be populated on rejection")

		// The position/cash from the approved submission are unchanged
		// by the rejected one.
		require.Len(t, result.SnapA.Positions(), 1)
		assert.True(t, result.SnapA.Positions()[0].Quantity.Equal(num.MustParseQuantity("10000")))

		require.Len(t, result.EventsA, 3, "the rejected submission produces zero additional events")
	})

	t.Run("account B: isolated from every account A operation", func(t *testing.T) {
		require.True(t, result.AccountBApprove.Proposal.Quantity.Equal(num.MustParseQuantity("5000")),
			"sized quantity: %s", result.AccountBApprove.Proposal.Quantity)

		snap := result.SnapB
		assert.True(t, snap.RealizedPnL().Equal(usd("0")))
		require.NotEmpty(t, snap.CashBalances(), "a regression here must fail this assertion, not panic on [0]")
		assert.True(t, snap.CashBalances()[0].Equal(usd("5000")), "B's starting cash is untouched by A's activity")

		require.Len(t, snap.Positions(), 1)
		pos := snap.Positions()[0]
		assert.Equal(t, order.Long, pos.Side)
		assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("5000")))
		assert.Equal(t, "GBP_USD", pos.Listing.Symbol(), "B never touches EUR_USD, A's own listing")

		require.Len(t, result.EventsB, 3)

		for _, p := range result.SnapA.Positions() {
			assert.NotEqual(t, "GBP_USD", p.Listing.Symbol(), "A must never carry B's own instrument")
		}
	})
}

// TestExecutionVerticalSlice_DeterministicAcrossRuns proves the entire
// scenario — account snapshots, complete event streams, and every
// intermediate SubmitResponse alike — is byte-for-byte reproducible:
// two independent runs, each constructing its own fresh Broker/
// Pipeline/Service from identical deterministic inputs, must produce
// identical observable results (#188's own "repeated deterministic
// runs are value/order equivalent" acceptance criterion).
func TestExecutionVerticalSlice_DeterministicAcrossRuns(t *testing.T) {
	first := runExecutionVerticalSliceScenario(t)
	second := runExecutionVerticalSliceScenario(t)

	require.Equal(t, first.FirstEvaluate, second.FirstEvaluate)
	require.Equal(t, first.Approved, second.Approved)
	require.Equal(t, first.Rejected, second.Rejected)
	require.Equal(t, first.RejectedErr, second.RejectedErr)
	require.Equal(t, first.AccountBApprove, second.AccountBApprove)
	require.Equal(t, first.SnapA, second.SnapA, "account A's final snapshot must be identical across runs")
	require.Equal(t, first.SnapB, second.SnapB, "account B's final snapshot must be identical across runs")
	require.Equal(t, first.EventsA, second.EventsA, "account A's complete event stream must be identical across runs")
	require.Equal(t, first.EventsB, second.EventsB, "account B's complete event stream must be identical across runs")
}
