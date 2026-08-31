package backtest_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	"github.com/rustyeddy/trader/strategy"
)

// capturingRecorder is an in-memory journal.Recorder collecting every
// Record it receives, in call order — the only way to observe the
// intents/proposals/decisions/requests/orders/fills a run produced,
// since backtest.Result itself exposes none of them directly (issue
// #223).
type capturingRecorder struct {
	mu      sync.Mutex
	records []journal.Record
}

func (r *capturingRecorder) Record(ctx context.Context, rec journal.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func (r *capturingRecorder) Close() error { return nil }

func (r *capturingRecorder) all() []journal.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]journal.Record(nil), r.records...)
}

// idNormalizer maps every opaque, run-local identifier it sees (in
// first-seen order, scoped to one run) to a stable "<kind>-<n>" token.
// Two independent runs with different id.Generator seeds necessarily
// produce different literal ULIDs for everything — RunID, IntentID,
// OrderID, FillID, AccountID, EventID/CorrelationID/CausationID — but
// an identically-shaped, identically-ordered run assigns those opaque
// identifiers in the identical *relative* order, so normalizing each
// run's own ID stream independently and comparing the resulting tokens
// is what proves "same causal graph," not "same execution identity"
// (issue #223 review, point 1/2).
type idNormalizer struct {
	tokens map[string]string
	next   map[string]int
}

func newIDNormalizer() *idNormalizer {
	return &idNormalizer{tokens: map[string]string{}, next: map[string]int{}}
}

func (n *idNormalizer) token(kind, raw string) string {
	if raw == "" {
		return ""
	}
	if tok, ok := n.tokens[kind+":"+raw]; ok {
		return tok
	}
	n.next[kind]++
	tok := kind + "-" + itoa(n.next[kind])
	n.tokens[kind+":"+raw] = tok
	return tok
}

func itoa(i int) string {
	// Avoids importing strconv solely for this; every count here is
	// small (well under 100 records per test run).
	digits := "0123456789"
	if i == 0 {
		return "0"
	}
	var buf []byte
	for i > 0 {
		buf = append([]byte{digits[i%10]}, buf...)
		i /= 10
	}
	return string(buf)
}

func (n *idNormalizer) run(v id.RunID) string       { return n.token("run", v.String()) }
func (n *idNormalizer) intent(v id.IntentID) string { return n.token("intent", v.String()) }
func (n *idNormalizer) event(v id.EventID) string   { return n.token("event", v.String()) }
func (n *idNormalizer) correlation(v id.CorrelationID) string {
	return n.token("correlation", v.String())
}
func (n *idNormalizer) account(v id.AccountID) string { return n.token("account", v.String()) }
func (n *idNormalizer) order(v id.OrderID) string     { return n.token("order", v.String()) }
func (n *idNormalizer) fill(v id.FillID) string       { return n.token("fill", v.String()) }

// brokerOrderID normalizes sim's own "sim-<OrderID>" BrokerOrderID
// convention (adapters/broker/sim/account.go) by normalizing the
// embedded OrderID and re-attaching the same "sim-" prefix, so it
// compares equal to the same token orderID would produce for the same
// underlying order.
func (n *idNormalizer) brokerOrderID(v string) string {
	const prefix = "sim-"
	if len(v) <= len(prefix) || v[:len(prefix)] != prefix {
		return n.token("broker-order-raw", v)
	}
	return prefix + n.token("order", v[len(prefix):])
}

func (n *idNormalizer) metadata(m id.Metadata) (event, correlation, causation string) {
	event = n.event(m.EventID)
	correlation = n.correlation(m.CorrelationID)
	if m.CausationID.IsZero() {
		causation = ""
	} else {
		causation = n.event(m.CausationID)
	}
	return
}

// requiredKinds is every journal.Kind the #223 acceptance criteria
// names that this fixture can actually produce, that the
// representative fixture below must actually produce at least one of
// — otherwise this golden could pass trivially because a whole
// category of events silently stopped being journaled (issue #223
// review, point 3). journal.KindAccount is deliberately excluded:
// Scheduler only journals it when the broker's own event stream emits
// a broker.EventKindAccount event (scheduler.go's own event-kind
// switch), and adapters/broker/sim never emits one — every simulated
// account-state change is observed only through the EventKindOrder/
// EventKindFill events that caused it, never a standalone "account
// changed" event. KindAccount is therefore architecturally
// unreachable from this (or any) sim.Broker-backed run today; asserting
// its presence here would either force an artificial event sim never
// produces or silently mislabel a real gap as this fixture's fault.
var requiredKinds = []journal.Kind{
	journal.KindRunStarted,
	journal.KindIntent,
	journal.KindProposal,
	journal.KindDecision,
	journal.KindRequest,
	journal.KindOrder,
	journal.KindFill,
	journal.KindTrade,
	journal.KindRunCompleted,
}

func assertRequiredKindsPresent(t *testing.T, records []journal.Record) {
	t.Helper()
	seen := map[journal.Kind]bool{}
	for _, rec := range records {
		seen[rec.Kind] = true
	}
	for _, k := range requiredKinds {
		assert.Truef(t, seen[k], "expected at least one %s record in the journal, found none — this golden cannot protect a kind it never observes", k)
	}
}

// enterThenExitStrategy enters long on the first bar it sees for each
// instrument, then exits on the second — producing one genuine closed
// round trip per instrument (issue #223 review, point 3: an enter-only
// fixture never exercises KindTrade's closed-trade shape, realized
// PnL, or the exit side of KindOrder/KindFill).
type enterThenExitStrategy struct {
	requirements []strategy.DataRequirement
	intents      strategy.IntentFactory
	entered      map[string]bool
	exited       map[string]bool
}

func (s *enterThenExitStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "enter-then-exit", Version: "test", Requirements: s.requirements}
}

func (s *enterThenExitStrategy) Start(ctx context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	s.entered = map[string]bool{}
	s.exited = map[string]bool{}
	return nil
}

func (s *enterThenExitStrategy) OnBar(ctx context.Context, ev strategy.BarEvent, view strategy.View) ([]order.Intent, error) {
	key := ev.Instrument.String()
	if !s.entered[key] {
		s.entered[key] = true
		in, err := s.intents.Enter(ev.Instrument, order.Buy)
		if err != nil {
			return nil, err
		}
		return []order.Intent{in}, nil
	}
	if !s.exited[key] {
		s.exited[key] = true
		ex, err := s.intents.Exit(ev.Instrument)
		if err != nil {
			return nil, err
		}
		return []order.Intent{ex}, nil
	}
	return nil, nil
}

// determinismRun is one independent run's full observable output: the
// Result, the captured journal, and this run's own idNormalizer.
type determinismRun struct {
	result backtest.Result
	rec    *capturingRecorder
	norm   *idNormalizer
}

// newDeterminismRunnerParams builds a fresh, independent RunnerParams
// seeded from (seed1, seed2) — deliberately given a *different* seed
// pair per run in the primary regression, so the two runs' RunIDs,
// AccountIDs, OrderIDs, and FillIDs are all genuinely distinct
// (issue #223 review, point 1), proving reproducibility is not merely
// an artifact of feeding both runs identical generators. A real,
// deterministic risk.Rule (NewMaxOpenPositionsRule) is configured —
// not a zero-rule Engine — so the captured KindDecision records
// exercise the canonical M4 risk path, not an always-trivially-allow
// stub (issue #223 review, point 4).
func newDeterminismRunnerParams(t *testing.T, mgr *marketdata.Manager, seed1, seed2 uint64, strat strategy.Strategy) (backtest.RunnerParams, *capturingRecorder) {
	t.Helper()
	ctx := context.Background()

	resolver := instrumentResolverFor(t)
	span := schedulerSpan(t)

	c := clock.NewSimulated(span.Start())
	ids := id.NewGenerator(c, id.NewDeterministic(seed1, seed2))
	accountID, err := id.GenerateAccountID(ids)
	require.NoError(t, err)

	b, err := sim.NewBroker("sim", sim.Deps{
		Clock: c,
		IDs:   ids,
		Prices: fixedPriceSource{
			"EUR_USD": num.MustParsePrice("1.10000"),
			"GBP_USD": num.MustParsePrice("1.27000"),
		},
	}, sim.AccountConfig{
		AccountID:    accountID,
		StartingCash: num.MustParseMoney("100000", num.MustParseCurrency("USD")),
	})
	require.NoError(t, err)

	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	planner, err := execution.NewPlanner(execution.Deps{Clock: c, IDs: ids})
	require.NoError(t, err)

	rule, err := risk.NewMaxOpenPositionsRule(5)
	require.NoError(t, err)
	engine, err := risk.NewEngine(rule)
	require.NoError(t, err)

	pl, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  b,
		IDs:     ids,
	})
	require.NoError(t, err)

	fill, slippage, commission := mustRunnerModels(t)
	ruleInfo, err := backtest.NewComponentInfo("max-open-positions", "v1", map[string]string{"max": "5"})
	require.NoError(t, err)

	rec := &capturingRecorder{}

	return backtest.RunnerParams{
		Manager:         mgr,
		Resolver:        resolver,
		Clock:           c,
		IDs:             ids,
		Pipeline:        pl,
		Account:         acc,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("0.01000"),
		RiskRules:       []backtest.ComponentInfo{ruleInfo},
		FillModel:       fill,
		SlippageModel:   slippage,
		CommissionModel: commission,
		Strategy:        strat,
		Span:            span,
		Journal:         rec,
	}, rec
}

// runDeterminismCase runs one independent backtest end to end, seeded
// from (seed1, seed2), returning its captured journal and result
// alongside a fresh idNormalizer for it.
func runDeterminismCase(t *testing.T, mgr *marketdata.Manager, seed1, seed2 uint64) determinismRun {
	t.Helper()
	strat := &enterThenExitStrategy{requirements: bothInstrumentsRequirements(t)}
	params, rec := newDeterminismRunnerParams(t, mgr, seed1, seed2, strat)

	runner, err := backtest.NewRunner(params)
	require.NoError(t, err)
	result, err := runner.Run(context.Background())
	require.NoError(t, err)

	return determinismRun{result: result, rec: rec, norm: newIDNormalizer()}
}

// TestBacktest_DeterministicAcrossIndependentRuns is #223's own
// completion-gate test: two independently constructed, differently
// seeded runs over identical data/strategy/parameters/risk/broker
// config must produce the same configuration identity and the same
// observable causal/economic outcome, without their execution
// identities (RunID, AccountID, every generated ID) ever needing to
// match.
func TestBacktest_DeterministicAcrossIndependentRuns(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	publishBothInstrumentsFixture(t, mgr)

	run1 := runDeterminismCase(t, mgr, 1, 2)
	run2 := runDeterminismCase(t, mgr, 101, 202)

	t.Run("execution identity differs, configuration identity does not", func(t *testing.T) {
		assert.False(t, run1.result.Manifest.RunID().Equal(run2.result.Manifest.RunID()),
			"two independently seeded runs must not share a RunID — RunID identifies an execution, not a configuration")
		assert.Equal(t, run1.result.Manifest.ConfigDigest(), run2.result.Manifest.ConfigDigest(),
			"identical configuration must produce identical ConfigDigest regardless of execution identity")
		assert.Equal(t, run1.result.Manifest.StrategyName(), run2.result.Manifest.StrategyName())
		assert.ElementsMatch(t, run1.result.Manifest.Universe(), run2.result.Manifest.Universe())
		assert.Equal(t, len(run1.result.Manifest.Dataset()), len(run2.result.Manifest.Dataset()))
		for i := range run1.result.Manifest.Dataset() {
			assert.Truef(t, run1.result.Manifest.Dataset()[i].Instrument.Equal(run2.result.Manifest.Dataset()[i].Instrument),
				"dataset[%d]: instrument mismatch", i)
			assert.Equalf(t, run1.result.Manifest.Dataset()[i].Revision(), run2.result.Manifest.Dataset()[i].Revision(),
				"dataset[%d]: revision mismatch", i)
		}
	})

	records1, records2 := run1.rec.all(), run2.rec.all()

	t.Run("journal contains every required event kind", func(t *testing.T) {
		assertRequiredKindsPresent(t, records1)
		assertRequiredKindsPresent(t, records2)
	})

	t.Run("journal", func(t *testing.T) {
		require.Equal(t, len(records1), len(records2), "journal record count must match")
		for i := range records1 {
			r1, r2 := records1[i], records2[i]
			require.Equalf(t, r1.Kind, r2.Kind, "record %d: kind mismatch", i)
			compareRecordSemantics(t, i, r1, r2, run1.norm, run2.norm)
		}
	})

	t.Run("trades", func(t *testing.T) {
		require.Equal(t, len(run1.result.Trades), len(run2.result.Trades), "closed trade count must match")
		require.NotEmpty(t, run1.result.Trades, "the enter-then-exit fixture must produce at least one closed trade")
		for i := range run1.result.Trades {
			compareTrades(t, i, run1.result.Trades[i], run2.result.Trades[i], run1.norm, run2.norm)
		}
		require.Equal(t, len(run1.result.OpenTrades), len(run2.result.OpenTrades), "open trade count must match")
	})

	t.Run("equity curve", func(t *testing.T) {
		require.Equal(t, len(run1.result.EquityCurve), len(run2.result.EquityCurve), "equity curve length must match")
		for i := range run1.result.EquityCurve {
			p1, p2 := run1.result.EquityCurve[i], run2.result.EquityCurve[i]
			assert.Truef(t, p1.Timestamp.Equal(p2.Timestamp), "equity_curve[%d]: timestamp mismatch: got %s want %s", i, p2.Timestamp, p1.Timestamp)
			assert.Truef(t, p1.Equity.Equal(p2.Equity), "equity_curve[%d]: equity mismatch: got %s want %s", i, p2.Equity, p1.Equity)
		}
	})

	t.Run("metrics", func(t *testing.T) {
		m1, m2 := run1.result.Metrics, run2.result.Metrics
		assert.True(t, m1.StartingCapital().Equal(m2.StartingCapital()), "metrics: starting capital mismatch")
		assert.True(t, m1.FinalEquity().Equal(m2.FinalEquity()), "metrics: final equity mismatch")
		assert.Equal(t, m1.NetReturn(), m2.NetReturn(), "metrics: net return mismatch")
		assert.Equal(t, m1.MaxDrawdown(), m2.MaxDrawdown(), "metrics: max drawdown mismatch")
		assert.Equal(t, m1.TradeCount(), m2.TradeCount(), "metrics: trade count mismatch")
		assert.Equal(t, m1.Wins(), m2.Wins(), "metrics: wins mismatch")
		assert.Equal(t, m1.Losses(), m2.Losses(), "metrics: losses mismatch")
		assert.True(t, m1.GrossPnL().Equal(m2.GrossPnL()), "metrics: gross pnl mismatch")
		assert.True(t, m1.ClosedTradeCosts().Equal(m2.ClosedTradeCosts()), "metrics: closed trade costs mismatch")
		assert.True(t, m1.AccountFees().Equal(m2.AccountFees()), "metrics: account fees mismatch")
		assert.True(t, m1.NetPnL().Equal(m2.NetPnL()), "metrics: net pnl mismatch")
		require.Equal(t, len(m1.PerInstrument()), len(m2.PerInstrument()), "metrics: per-instrument count mismatch")
		for i := range m1.PerInstrument() {
			pi1, pi2 := m1.PerInstrument()[i], m2.PerInstrument()[i]
			assert.Truef(t, pi1.InstrumentID.Equal(pi2.InstrumentID), "metrics.per_instrument[%d]: instrument mismatch", i)
			assert.Equalf(t, pi1.Count, pi2.Count, "metrics.per_instrument[%d]: count mismatch", i)
			assert.Truef(t, pi1.NetPnL.Equal(pi2.NetPnL), "metrics.per_instrument[%d]: net pnl mismatch", i)
		}
	})

	t.Run("account", func(t *testing.T) {
		a1, a2 := run1.result.Account, run2.result.Account
		assert.True(t, a1.Equity().Equal(a2.Equity()), "account: equity mismatch")
		assert.True(t, a1.RealizedPnL().Equal(a2.RealizedPnL()), "account: realized pnl mismatch")
		assert.True(t, a1.UnrealizedPnL().Equal(a2.UnrealizedPnL()), "account: unrealized pnl mismatch")
		assert.True(t, a1.Fees().Equal(a2.Fees()), "account: fees mismatch")
		assert.True(t, a1.AsOf().Equal(a2.AsOf()), "account: as-of timestamp mismatch")
		require.Equal(t, len(a1.Positions()), len(a2.Positions()), "account: position count mismatch")
		for i := range a1.Positions() {
			p1, p2 := a1.Positions()[i], a2.Positions()[i]
			assert.Equalf(t, p1.Side, p2.Side, "account.positions[%d]: side mismatch", i)
			assert.Truef(t, p1.Quantity.Equal(p2.Quantity), "account.positions[%d]: quantity mismatch", i)
		}
	})
}

// compareRecordSemantics compares r1/r2 (already known to share a
// Kind) field by field, normalizing every opaque ID through each
// run's own idNormalizer first — never comparing raw ULIDs, which are
// expected to differ between two differently-seeded runs (issue #223
// review, point 2).
func compareRecordSemantics(t *testing.T, i int, r1, r2 journal.Record, n1, n2 *idNormalizer) {
	t.Helper()

	e1, c1, cause1 := n1.metadata(r1.Metadata)
	e2, c2, cause2 := n2.metadata(r2.Metadata)
	assert.Equalf(t, e1, e2, "record[%d]: metadata event id shape mismatch", i)
	assert.Equalf(t, c1, c2, "record[%d]: metadata correlation id shape mismatch", i)
	assert.Equalf(t, cause1, cause2, "record[%d]: metadata causation id shape mismatch", i)
	assert.Truef(t, r1.Metadata.Timestamp.Equal(r2.Metadata.Timestamp), "record[%d]: metadata timestamp mismatch: got %s want %s", i, r2.Metadata.Timestamp, r1.Metadata.Timestamp)

	switch r1.Kind {
	case journal.KindRunStarted:
		assert.Equalf(t, n1.run(r1.RunStarted.RunID), n2.run(r2.RunStarted.RunID), "record[%d]/run_started: run id shape mismatch", i)
	case journal.KindIntent:
		in1, in2 := r1.Intent, r2.Intent
		assert.Equalf(t, in1.Kind, in2.Kind, "record[%d]/intent: kind mismatch", i)
		assert.Truef(t, in1.Instrument.Equal(in2.Instrument), "record[%d]/intent: instrument mismatch", i)
		assert.Equalf(t, in1.Side, in2.Side, "record[%d]/intent: side mismatch", i)
		assert.Equalf(t, n1.intent(in1.IntentID), n2.intent(in2.IntentID), "record[%d]/intent: intent id shape mismatch", i)
	case journal.KindProposal:
		p1, p2 := r1.Proposal, r2.Proposal
		assert.Truef(t, p1.Listing.InstrumentID().Equal(p2.Listing.InstrumentID()), "record[%d]/proposal: instrument mismatch", i)
		assert.Equalf(t, p1.Side, p2.Side, "record[%d]/proposal: side mismatch", i)
		assert.Equalf(t, p1.Type, p2.Type, "record[%d]/proposal: type mismatch", i)
		assert.Equalf(t, p1.TimeInForce, p2.TimeInForce, "record[%d]/proposal: time in force mismatch", i)
		assert.Truef(t, p1.Quantity.Equal(p2.Quantity), "record[%d]/proposal: quantity mismatch: got %s want %s", i, p2.Quantity, p1.Quantity)
		assert.Equalf(t, n1.account(p1.AccountID), n2.account(p2.AccountID), "record[%d]/proposal: account id shape mismatch", i)
	case journal.KindDecision:
		d1, d2 := r1.Decision, r2.Decision
		assert.Equalf(t, d1.Allowed, d2.Allowed, "record[%d]/decision: allowed mismatch", i)
		assert.Equalf(t, len(d1.Violations), len(d2.Violations), "record[%d]/decision: violation count mismatch", i)
		assert.Equalf(t, len(d1.RuleResults), len(d2.RuleResults), "record[%d]/decision: rule result count mismatch", i)
		for j := range d1.RuleResults {
			assert.Equalf(t, d1.RuleResults[j].Rule, d2.RuleResults[j].Rule, "record[%d]/decision: rule_results[%d] name mismatch", i, j)
		}
	case journal.KindRequest:
		req1, req2 := r1.Request, r2.Request
		assert.Truef(t, req1.Listing.InstrumentID().Equal(req2.Listing.InstrumentID()), "record[%d]/request: instrument mismatch", i)
		assert.Equalf(t, req1.Side, req2.Side, "record[%d]/request: side mismatch", i)
		assert.Truef(t, req1.Quantity.Equal(req2.Quantity), "record[%d]/request: quantity mismatch", i)
		assert.Equalf(t, n1.order(req1.OrderID), n2.order(req2.OrderID), "record[%d]/request: order id shape mismatch", i)
	case journal.KindOrder:
		o1, o2 := r1.Order, r2.Order
		assert.Equalf(t, o1.Status, o2.Status, "record[%d]/order: status mismatch", i)
		assert.Equalf(t, n1.order(o1.Request.OrderID), n2.order(o2.Request.OrderID), "record[%d]/order: order id shape mismatch", i)
		assert.Equalf(t, n1.brokerOrderID(o1.BrokerOrderID), n2.brokerOrderID(o2.BrokerOrderID), "record[%d]/order: broker order id shape mismatch", i)
		if o1.AcceptedQuantity != nil && o2.AcceptedQuantity != nil {
			assert.Truef(t, o1.AcceptedQuantity.Equal(*o2.AcceptedQuantity), "record[%d]/order: accepted quantity mismatch", i)
		} else {
			assert.Equalf(t, o1.AcceptedQuantity == nil, o2.AcceptedQuantity == nil, "record[%d]/order: accepted quantity nilness mismatch", i)
		}
	case journal.KindFill:
		f1, f2 := r1.Fill, r2.Fill
		assert.Truef(t, f1.Listing.InstrumentID().Equal(f2.Listing.InstrumentID()), "record[%d]/fill: instrument mismatch", i)
		assert.Equalf(t, f1.Side, f2.Side, "record[%d]/fill: side mismatch", i)
		assert.Truef(t, f1.Price.Equal(f2.Price), "record[%d]/fill: price mismatch: got %s want %s", i, f2.Price, f1.Price)
		assert.Truef(t, f1.Quantity.Equal(f2.Quantity), "record[%d]/fill: quantity mismatch", i)
		assert.Equalf(t, n1.fill(f1.FillID), n2.fill(f2.FillID), "record[%d]/fill: fill id shape mismatch", i)
		assert.Equalf(t, n1.order(f1.OrderID), n2.order(f2.OrderID), "record[%d]/fill: order id shape mismatch", i)
		assert.Equalf(t, n1.account(f1.AccountID), n2.account(f2.AccountID), "record[%d]/fill: account id shape mismatch", i)
	case journal.KindAccount:
		a1, a2 := r1.Account, r2.Account
		assert.Truef(t, a1.Equity().Equal(a2.Equity()), "record[%d]/account: equity mismatch: got %s want %s", i, a2.Equity(), a1.Equity())
		assert.Equalf(t, n1.account(a1.AccountID()), n2.account(a2.AccountID()), "record[%d]/account: account id shape mismatch", i)
	case journal.KindTrade:
		compareTrades(t, i, *r1.Trade, *r2.Trade, n1, n2)
	case journal.KindRunCompleted:
		assert.Equalf(t, r1.RunCompleted.EntryCount, r2.RunCompleted.EntryCount, "record[%d]/run_completed: entry count mismatch", i)
	}
}

// compareTrades compares two order.Trade values (either two closed
// trades at the same index, or the payload of two KindTrade journal
// records), normalizing AccountID/fill IDs and comparing every
// economically meaningful field directly.
func compareTrades(t *testing.T, i int, tr1, tr2 order.Trade, n1, n2 *idNormalizer) {
	t.Helper()
	assert.Truef(t, tr1.Listing.InstrumentID().Equal(tr2.Listing.InstrumentID()), "trade[%d]: instrument mismatch", i)
	assert.Equalf(t, tr1.Side, tr2.Side, "trade[%d]: side mismatch", i)
	assert.Truef(t, tr1.OpenedAt.Equal(tr2.OpenedAt), "trade[%d]: opened_at mismatch: got %s want %s", i, tr2.OpenedAt, tr1.OpenedAt)
	assert.Truef(t, tr1.ClosedAt.Equal(tr2.ClosedAt), "trade[%d]: closed_at mismatch: got %s want %s", i, tr2.ClosedAt, tr1.ClosedAt)
	assert.Truef(t, tr1.RealizedPnL.Equal(tr2.RealizedPnL), "trade[%d]: realized pnl mismatch: got %s want %s", i, tr2.RealizedPnL, tr1.RealizedPnL)
	assert.Truef(t, tr1.Costs.Equal(tr2.Costs), "trade[%d]: costs mismatch: got %s want %s", i, tr2.Costs, tr1.Costs)
	assert.Equalf(t, n1.account(tr1.AccountID), n2.account(tr2.AccountID), "trade[%d]: account id shape mismatch", i)

	require.Equalf(t, len(tr1.EntryFillIDs), len(tr2.EntryFillIDs), "trade[%d]: entry fill count mismatch", i)
	for j := range tr1.EntryFillIDs {
		assert.Equalf(t, n1.fill(tr1.EntryFillIDs[j]), n2.fill(tr2.EntryFillIDs[j]), "trade[%d]: entry_fill_ids[%d] shape mismatch", i, j)
	}
	require.Equalf(t, len(tr1.ExitFillIDs), len(tr2.ExitFillIDs), "trade[%d]: exit fill count mismatch", i)
	for j := range tr1.ExitFillIDs {
		assert.Equalf(t, n1.fill(tr1.ExitFillIDs[j]), n2.fill(tr2.ExitFillIDs[j]), "trade[%d]: exit_fill_ids[%d] shape mismatch", i, j)
	}
}

// TestManifest_ConfigDigestChangesWhenDatasetRevisionDiffers is the
// dataset-identity contrapositive (issue #223 review, point 6): two
// Manifests identical in every field except one Dataset entry's own
// Revision must produce different ConfigDigest values, proving
// ConfigDigest actually incorporates dataset identity rather than
// merely happening to pass because a fixture never varies it.
func TestManifest_ConfigDigestChangesWhenDatasetRevisionDiffers(t *testing.T) {
	base := baseManifestParams(t)
	m1, err := backtest.NewManifest(base)
	require.NoError(t, err)

	withDifferentRevision := base
	changed := mustDatasetManifestFor(t, marketdata.H1, mustManifestSpan(t))
	changed.RawFingerprint = "sha256:0000000000000000000000000000000000000000000000000000000000000000000000000000"
	withDifferentRevision.Dataset = []marketdata.Manifest{changed}
	m2, err := backtest.NewManifest(withDifferentRevision)
	require.NoError(t, err)

	assert.NotEqual(t, m1.ConfigDigest(), m2.ConfigDigest(), "changing a dataset entry's own revision/fingerprint must change ConfigDigest")
}

// TestManifest_ConfigDigestChangesWhenDatasetInstrumentDiffers proves
// ConfigDigest also depends on *which* dataset (instrument/interval),
// not only its revision — a second, independent identity field
// participating in canonical dataset identity per ADR-020 (issue #223
// review, point 6).
func TestManifest_ConfigDigestChangesWhenDatasetInstrumentDiffers(t *testing.T) {
	base := baseManifestParams(t)
	m1, err := backtest.NewManifest(base)
	require.NoError(t, err)

	withDifferentInstrument := base
	withDifferentInstrument.Universe = []strategy.DataRequirement{
		{Instrument: gbpusdID(t), Interval: marketdata.H1, WarmupBars: 26},
	}
	withDifferentInstrument.Dataset = []marketdata.Manifest{mustDatasetManifestFor(t, marketdata.H1, mustManifestSpan(t))}
	withDifferentInstrument.Dataset[0].Instrument = gbpusdID(t)
	require.NoError(t, withDifferentInstrument.Dataset[0].Validate())
	m2, err := backtest.NewManifest(withDifferentInstrument)
	require.NoError(t, err)

	assert.NotEqual(t, m1.ConfigDigest(), m2.ConfigDigest(), "changing the dataset's own instrument must change ConfigDigest")
}

// Note: ConfigDigest's independence from RunID is already covered by
// TestManifest_ConfigDigestIgnoresRunID (manifest_test.go) — not
// duplicated here.
