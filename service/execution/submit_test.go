package execution

import (
	"context"
	"errors"
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

var testStart = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

type fixedPriceSource map[string]num.Price

func (f fixedPriceSource) Info() sim.ModelInfo {
	return sim.ModelInfo{Name: "fixedPriceSource", Version: "test"}
}

func (f fixedPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	p, ok := f[listing.Symbol()]
	if !ok {
		return num.Price{}, errors.New("fixedPriceSource: no price")
	}
	return p, nil
}

func mustEurUsdListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
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
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

// testHarness bundles a deterministic sim.Broker plus a Pipeline built
// on top of it, mirroring how a real composition root wires both from
// the same underlying broker instance.
type testHarness struct {
	broker    *sim.Broker
	accountID id.AccountID
	listing   instrument.Listing
	ids       *id.Generator
	clock     clock.Clock
}

func newHarness(t *testing.T, brokerName, startingCash string) testHarness {
	t.Helper()
	c := clock.NewSimulated(testStart)
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(ids)
	require.NoError(t, err)

	b, err := sim.NewBroker(brokerName, sim.Deps{
		Clock: c,
		IDs:   ids,
		Prices: fixedPriceSource{
			"EUR_USD": num.MustParsePrice("1.10000"),
		},
	}, sim.AccountConfig{
		AccountID:    accountID,
		StartingCash: num.MustParseMoney(startingCash, num.MustParseCurrency("USD")),
	})
	require.NoError(t, err)

	return testHarness{
		broker:    b,
		accountID: accountID,
		listing:   mustEurUsdListing(t),
		ids:       ids,
		clock:     c,
	}
}

func (h testHarness) snapshot(t *testing.T, ctx context.Context) account.Snapshot {
	t.Helper()
	acc, err := h.broker.OpenAccount(ctx, h.accountID)
	require.NoError(t, err)
	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	return snap
}

func newPipelineOver(t *testing.T, b brokerpkg.Broker, ids *id.Generator, c clock.Clock, rules ...risk.Rule) *pipeline.Pipeline {
	t.Helper()
	planner, err := executionpkg.NewPlanner(executionpkg.Deps{Clock: c, IDs: ids})
	require.NoError(t, err)
	engine, err := risk.NewEngine(rules...)
	require.NoError(t, err)
	p, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  b,
		IDs:     ids,
	})
	require.NoError(t, err)
	return p
}

func mustEnterIntent(t *testing.T, ids *id.Generator, instID instrument.ID, side order.Side) order.Intent {
	t.Helper()
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
		Side:       side,
		Metadata:   id.Metadata{EventID: eventID, CorrelationID: corrID},
	})
	require.NoError(t, err)
	return in
}

func TestSubmit_EndToEndAgainstSimulator(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock)
	svc, err := New(h.broker, p, nil)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	resp, err := svc.Submit(ctx, SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.NoError(t, err)
	assert.True(t, resp.Decision.Allowed)
	assert.NotEqual(t, order.Order{}, resp.Order)
	assert.Equal(t, resp.Proposal.Quantity, resp.Order.Request.Quantity)

	after := h.snapshot(t, ctx)
	require.Len(t, after.Positions(), 1)
}

// TestEvaluate_EndToEndNeverMutatesBroker is #187's own driving
// requirement: Evaluate must return the same Proposal/Decision/Request
// Submit would, without ever opening the broker account for a write or
// calling Submit on it — Order stays zero, and the account is
// untouched.
func TestEvaluate_EndToEndNeverMutatesBroker(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock)
	svc, err := New(h.broker, p, nil)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	resp, err := svc.Evaluate(ctx, SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.NoError(t, err)
	assert.True(t, resp.Decision.Allowed)
	assert.NotEqual(t, order.Request{}, resp.Request)
	assert.False(t, resp.Request.OrderID.IsZero())
	assert.Equal(t, order.Order{}, resp.Order, "Evaluate must never populate Order")

	after := h.snapshot(t, ctx)
	assert.Empty(t, after.Positions(), "Evaluate must never mutate the broker")
}

// TestEvaluate_RejectionLeavesRequestZero mirrors
// TestSubmit_RiskRejectionReturnsStructuredResultButNoOrder for
// Evaluate.
func TestEvaluate_RejectionLeavesRequestZero(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock, rejectingRule{})
	svc, err := New(h.broker, p, nil)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	resp, err := svc.Evaluate(ctx, SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, pipeline.ErrRejected)
	assert.False(t, resp.Decision.Allowed)
	assert.Equal(t, order.Request{}, resp.Request)
	assert.Equal(t, order.Order{}, resp.Order)
}

// TestSubmit_UsesExactRequestEvaluateBuilt is the service-layer
// analog of pipeline's own TestPipelineSubmit_UsesExactRequestEvaluateBuilt:
// Submit's own Result.Request must equal Result.Order.Request exactly,
// proving Submit never re-derives a second Request independently of
// what Evaluate already built.
func TestSubmit_UsesExactRequestEvaluateBuilt(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock)
	svc, err := New(h.broker, p, nil)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	resp, err := svc.Submit(ctx, SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.NoError(t, err)
	assert.Equal(t, resp.Request, resp.Order.Request)
}

func TestEvaluate_InvalidRequestAccountIDZero(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock)
	svc, err := New(h.broker, p, nil)
	require.NoError(t, err)

	_, err = svc.Evaluate(ctx, SubmitRequest{Listing: h.listing})
	require.ErrorIs(t, err, ErrInvalidRequest)
}

type rejectingRule struct{}

func (rejectingRule) Name() string { return "always_reject" }
func (rejectingRule) Evaluate(ctx context.Context, in risk.Input) (risk.RuleResult, error) {
	return risk.RuleResult{Violations: []risk.Violation{{Message: "test: always rejects"}}}, nil
}

func TestSubmit_RiskRejectionReturnsStructuredResultButNoOrder(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock, rejectingRule{})
	svc, err := New(h.broker, p, nil)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	resp, err := svc.Submit(ctx, SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, pipeline.ErrRejected)
	assert.False(t, resp.Decision.Allowed)
	assert.NotEqual(t, order.Proposal{}, resp.Proposal)
	assert.Equal(t, order.Order{}, resp.Order)

	after := h.snapshot(t, ctx)
	assert.Empty(t, after.Positions())
}

func TestSubmit_InvalidRequestAccountIDZero(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock)
	svc, err := New(h.broker, p, nil)
	require.NoError(t, err)

	_, err = svc.Submit(ctx, SubmitRequest{Listing: h.listing})
	require.ErrorIs(t, err, ErrInvalidRequest)
}

// failingBroker/failingAccount are minimal brokerpkg.Broker/Account
// doubles with fully controlled error fields, used to exercise
// Submit's own OpenAccount/Snapshot failure paths deterministically —
// the real sim.Broker has no way to fail either for a structurally
// valid, known account.
type failingBroker struct {
	name    string
	openErr error
	snapErr error
}

func (b failingBroker) Name() string {
	if b.name == "" {
		return "sim"
	}
	return b.name
}

func (b failingBroker) Accounts(ctx context.Context) ([]account.Reference, error) { return nil, nil }

func (b failingBroker) OpenAccount(ctx context.Context, accountID id.AccountID) (brokerpkg.Account, error) {
	if b.openErr != nil {
		return nil, b.openErr
	}
	return failingAccount{snapErr: b.snapErr}, nil
}

func (b failingBroker) Close() error { return nil }

type failingAccount struct {
	snapErr error
}

func (a failingAccount) Reference() account.Reference { return account.Reference{} }

func (a failingAccount) Snapshot(ctx context.Context) (account.Snapshot, error) {
	if a.snapErr != nil {
		return account.Snapshot{}, a.snapErr
	}
	return account.Snapshot{}, errors.New("not implemented")
}

func (a failingAccount) Submit(ctx context.Context, req order.Request) (order.Order, error) {
	return order.Order{}, errors.New("not implemented")
}

func (a failingAccount) Cancel(ctx context.Context, req order.CancelRequest) (order.CancelResult, error) {
	return order.CancelResult{}, errors.New("not implemented")
}

func (a failingAccount) Replace(ctx context.Context, req order.ReplaceRequest) (order.ReplaceResult, error) {
	return order.ReplaceResult{}, errors.New("not implemented")
}

func (a failingAccount) Events(ctx context.Context, cursor brokerpkg.EventCursor) (brokerpkg.EventReader, error) {
	return nil, errors.New("not implemented")
}

var _ brokerpkg.Broker = failingBroker{}
var _ brokerpkg.Account = failingAccount{}

func TestSubmit_OpenAccountErrorPropagates(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "sim", "10000")
	openErr := errors.New("boom: open account")
	fb := failingBroker{openErr: openErr}
	p := newPipelineOver(t, fb, h.ids, h.clock)
	svc, err := New(fb, p, nil)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	_, err = svc.Submit(ctx, SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, openErr)
}

func TestSubmit_SnapshotErrorPropagates(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "sim", "10000")
	snapErr := errors.New("boom: snapshot")
	fb := failingBroker{snapErr: snapErr}
	p := newPipelineOver(t, fb, h.ids, h.clock)
	svc, err := New(fb, p, nil)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	_, err = svc.Submit(ctx, SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, snapErr)
}

// TestSubmit_MismatchedBrokerWiringRejectedByPipeline is #186's own
// requested regression: Service is wired with one broker.Broker (used
// to fetch the snapshot) and a Pipeline backed by a *different*
// broker.Broker. This must never silently submit through whichever
// broker the mismatched Pipeline happens to hold — Pipeline's own
// broker/account identity check (PR #200) rejects it before any
// mutation, and that rejection must surface through Service unharmed,
// not merely be prevented by composition-root discipline.
func TestSubmit_MismatchedBrokerWiringRejectedByPipeline(t *testing.T) {
	ctx := context.Background()
	hA := newHarness(t, "simA", "10000")
	hB := newHarness(t, "simB", "10000")

	// Pipeline is backed by broker B, but Service's own broker (used to
	// fetch the snapshot) is broker A.
	p := newPipelineOver(t, hB.broker, hA.ids, hA.clock)
	svc, err := New(hA.broker, p, nil)
	require.NoError(t, err)

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, hA.ids, hA.listing.InstrumentID(), order.Buy)

	_, err = svc.Submit(ctx, SubmitRequest{
		AccountID:       hA.accountID,
		Intent:          intent,
		Listing:         hA.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, pipeline.ErrInvalidInput)

	afterA := hA.snapshot(t, ctx)
	assert.Empty(t, afterA.Positions(), "broker A must never be mutated by a mismatched-wiring submission")

	afterB := hB.snapshot(t, ctx)
	assert.Empty(t, afterB.Positions(), "broker B must never be opened/submitted to either — the mismatch is rejected before any broker call")
}

func TestSubmit_PropagatesCancelledContext(t *testing.T) {
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock)
	svc, err := New(h.broker, p, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	adverse := num.MustParsePrice("0.01000")
	intent := mustEnterIntent(t, h.ids, h.listing.InstrumentID(), order.Buy)

	_, err = svc.Submit(ctx, SubmitRequest{
		AccountID:       h.accountID,
		Intent:          intent,
		Listing:         h.listing,
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: &adverse,
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestNew_RejectsNilDeps(t *testing.T) {
	h := newHarness(t, "sim", "10000")
	p := newPipelineOver(t, h.broker, h.ids, h.clock)

	_, err := New(nil, p, nil)
	require.ErrorIs(t, err, ErrNilBroker)

	_, err = New(h.broker, nil, nil)
	require.ErrorIs(t, err, ErrNilPipeline)
}
