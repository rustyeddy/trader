package backtest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/backtest"
	brokerpkg "github.com/rustyeddy/trader/internal/broker"
	"github.com/rustyeddy/trader/internal/execution"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/internal/journal"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/pipeline"
	"github.com/rustyeddy/trader/internal/risk"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// marginInvariantAccount checks ADR-066's fill invariant from outside
// the simulator: after every Submit or AdvanceBar that grew any
// listing's position, margin used (gross notional × ratio at current
// marks, with the filled listing at its fill price) must not exceed
// equity. Calls that only move prices are exempt — v1 has no
// maintenance margin, so drift may legitimately breach the limit.
type marginInvariantAccount struct {
	brokerpkg.Account
	observer backtest.MarketObserver
	advancer backtest.IntrabarAdvancer

	t          *testing.T
	fills      int // calls that grew a position
	violations []string
}

func newMarginInvariantAccount(t *testing.T, acc brokerpkg.Account) *marginInvariantAccount {
	return &marginInvariantAccount{
		Account:  acc,
		observer: acc.(backtest.MarketObserver),
		advancer: acc.(backtest.IntrabarAdvancer),
		t:        t,
	}
}

func (a *marginInvariantAccount) quantities(ctx context.Context) map[account.ListingKey]num.Quantity {
	s, err := a.Snapshot(ctx)
	require.NoError(a.t, err)
	q := make(map[account.ListingKey]num.Quantity)
	for _, p := range s.Positions() {
		q[account.KeyOf(p.Listing)] = p.Quantity
	}
	return q
}

func (a *marginInvariantAccount) check(ctx context.Context, before map[account.ListingKey]num.Quantity) {
	grew := false
	for k, q := range a.quantities(ctx) {
		if q.Cmp(before[k]) > 0 {
			grew = true
		}
	}
	if !grew {
		return
	}
	a.fills++
	s, err := a.Snapshot(ctx)
	require.NoError(a.t, err)
	cmp, err := s.MarginUsed().Cmp(s.Equity())
	require.NoError(a.t, err)
	if cmp > 0 {
		a.violations = append(a.violations, "margin used "+s.MarginUsed().String()+" exceeds equity "+s.Equity().String()+" at "+s.AsOf().String())
	}
}

func (a *marginInvariantAccount) Submit(ctx context.Context, req runtimeorder.Request) (runtimeorder.Order, error) {
	before := a.quantities(ctx)
	o, err := a.Account.Submit(ctx, req)
	if err == nil {
		a.check(ctx, before)
	}
	return o, err
}

func (a *marginInvariantAccount) ObserveMark(ctx context.Context, instrumentID instrument.ID, close num.Price, at time.Time) error {
	return a.observer.ObserveMark(ctx, instrumentID, close, at)
}

func (a *marginInvariantAccount) AdvanceBar(ctx context.Context, listing instrument.Listing, open, high, low, close num.Price, at time.Time) error {
	before := a.quantities(ctx)
	err := a.advancer.AdvanceBar(ctx, listing, open, high, low, close, at)
	if err == nil {
		a.check(ctx, before)
	}
	return err
}

// marginRun is one runMarginInvariant run's observable outputs.
type marginRun struct {
	acc   *marginInvariantAccount
	sched *backtest.Scheduler
	rec   *capturingRecorder
}

// runMarginInvariant runs strat at initial_margin_ratio 1.0 with every
// order submitted through a marginInvariantAccount, sizing each entry
// at riskFraction of 100000 over a 0.01 adverse distance. rules are the
// risk engine's rules (none: only the simulator enforces margin).
func runMarginInvariant(t *testing.T, strat strategy.Strategy, riskFraction string, rules ...risk.Rule) marginRun {
	t.Helper()
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	one := num.MustParseRate("1")
	h := newSchedulerHarnessWithMargin(t, schedulerSpan(t).Start(), &one)
	deps := newSchedulerDeps(t, replay, strat, h)
	checked := newMarginInvariantAccount(t, deps.Account)
	deps.Account = checked
	deps.MarketObserver = checked
	deps.IntrabarAdvancer = checked
	builder := deps.Builder.(fixedInputBuilder)
	builder.riskFraction = num.MustParseRate(riskFraction)
	deps.Builder = builder

	// The pipeline must submit through the checked account too.
	planner, err := execution.NewPlanner(execution.Deps{Clock: h.clockObj, IDs: h.ids})
	require.NoError(t, err)
	engine, err := risk.NewEngine(rules...)
	require.NoError(t, err)
	deps.Pipeline, err = pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  &checkedBroker{Broker: h.broker, account: checked},
		IDs:     h.ids,
	})
	require.NoError(t, err)

	rec := &capturingRecorder{}
	deps.Journal = rec

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()))
	return marginRun{acc: checked, sched: sched, rec: rec}
}

// checkedBroker hands out the invariant-checking account.
type checkedBroker struct {
	brokerpkg.Broker
	account *marginInvariantAccount
}

func (b *checkedBroker) OpenAccount(ctx context.Context, accountID id.AccountID) (brokerpkg.Account, error) {
	return b.account, nil
}

// TestMarginInvariant_BuyAndHold: one entry per instrument, sized to
// fit (0.3% risk: 30000 units, about 0.33x equity each).
func TestMarginInvariant_BuyAndHold(t *testing.T) {
	acc := runMarginInvariant(t, mustEnterOnFirstBarStrategy(t), "0.003").acc
	assert.Positive(t, acc.fills, "the fixture must actually fill")
	assert.Empty(t, acc.violations)
}

// TestMarginInvariant_MultiInstrumentRepeatedEntries enters both
// instruments on every bar, so exposure keeps growing until the
// simulator refuses fills; no fill may ever leave the account over the
// limit.
func TestMarginInvariant_MultiInstrumentRepeatedEntries(t *testing.T) {
	strat := &recordingStrategy{
		requirements: bothInstrumentsRequirements(t),
		emit: func(f strategy.IntentFactory, ev strategy.BarEvent) ([]runtimeorder.Intent, error) {
			in, err := f.Enter(ev.Instrument, order.Buy)
			if err != nil {
				return nil, err
			}
			return []runtimeorder.Intent{in}, nil
		},
	}
	acc := runMarginInvariant(t, strat, "0.003").acc
	assert.Positive(t, acc.fills)
	assert.Empty(t, acc.violations)

	s, err := acc.Snapshot(context.Background())
	require.NoError(t, err)
	cmp, err := s.MarginUsed().Cmp(s.Equity())
	require.NoError(t, err)
	assert.LessOrEqual(t, cmp, 0)
	var gross num.Quantity
	for _, p := range s.Positions() {
		gross, err = gross.Add(p.Quantity)
		require.NoError(t, err)
	}
	assert.True(t, gross.Cmp(num.MustParseQuantity("30000")) > 0, "more than one entry filled, so the limit was actually approached")
}

// enterEveryBar enters both instruments long on every bar, so exposure
// keeps growing until margin refuses it.
func enterEveryBar(t *testing.T) *recordingStrategy {
	return &recordingStrategy{
		requirements: bothInstrumentsRequirements(t),
		emit: func(f strategy.IntentFactory, ev strategy.BarEvent) ([]runtimeorder.Intent, error) {
			in, err := f.Enter(ev.Instrument, order.Buy)
			if err != nil {
				return nil, err
			}
			return []runtimeorder.Intent{in}, nil
		},
	}
}

// journalMarginCounts counts margin refusals independently of the
// Scheduler, from the journal it wrote.
func journalMarginCounts(records []journal.Record) (admission, fill int) {
	for _, r := range records {
		switch r.Kind {
		case journal.KindDecision:
			if r.Decision.Allowed {
				continue
			}
			for _, v := range r.Decision.Violations {
				if v.Rule == risk.AccountInitialMarginRuleName {
					admission++
					break
				}
			}
		case journal.KindOrder:
			o := r.Order
			if (o.Status == runtimeorder.StatusRejected && o.Rejection != nil && o.Rejection.Reason == runtimeorder.ReasonInsufficientMargin) ||
				(o.Status == runtimeorder.StatusCanceled && o.CancelReason != nil && o.CancelReason.Reason == runtimeorder.ReasonInsufficientMargin) {
				fill++
			}
		}
	}
	return admission, fill
}

// TestMarginReporting_FillTimeRejections: with no risk rule, only the
// simulator refuses, so every refusal is fill-time.
func TestMarginReporting_FillTimeRejections(t *testing.T) {
	run := runMarginInvariant(t, enterEveryBar(t), "0.003")
	got := run.sched.MarginRejections()
	admission, fill := journalMarginCounts(run.rec.all())
	assert.Zero(t, got.Admission)
	assert.Positive(t, got.Fill, "the growing entries must eventually be refused")
	assert.Equal(t, fill, got.Fill)
	assert.Equal(t, admission, got.Admission)
}

// TestMarginReporting_AdmissionRejections: with the risk rule
// installed, over-limit entries are rejected at admission.
func TestMarginReporting_AdmissionRejections(t *testing.T) {
	rule, err := risk.NewAccountInitialMarginRule(num.MustParseRate("1"))
	require.NoError(t, err)
	run := runMarginInvariant(t, enterEveryBar(t), "0.003", rule)
	got := run.sched.MarginRejections()
	admission, fill := journalMarginCounts(run.rec.all())
	assert.Positive(t, got.Admission)
	assert.Equal(t, admission, got.Admission)
	assert.Equal(t, fill, got.Fill)
	assert.Equal(t, got.Admission+got.Fill, got.Total())
}

// TestMarginReporting_EquityCurveCarriesGrossNotional: every per-bar
// point carries the account's gross notional, and it grows as entries
// fill, never exceeding equity at ratio 1.0 on a filled bar.
func TestMarginReporting_EquityCurveCarriesGrossNotional(t *testing.T) {
	run := runMarginInvariant(t, enterEveryBar(t), "0.003")
	curve := run.sched.EquityCurve()
	require.NotEmpty(t, curve)
	var peak num.Money
	for i, p := range curve {
		require.NotNil(t, p.GrossNotional, "point %d", i)
		if i == 0 {
			peak = *p.GrossNotional
			continue
		}
		cmp, err := p.GrossNotional.Cmp(peak)
		require.NoError(t, err)
		if cmp > 0 {
			peak = *p.GrossNotional
		}
	}
	assert.False(t, peak.IsZero(), "positions were opened")
}
