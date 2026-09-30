package pipeline_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/execution"
	"github.com/rustyeddy/trader/internal/id"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/pipeline"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

func mustKindIntent(t *testing.T, ids *id.Generator, instID instrument.ID, mutate func(*runtimeorder.Intent)) runtimeorder.Intent {
	t.Helper()
	intentID, err := id.GenerateIntentID(ids)
	require.NoError(t, err)
	eventID, err := id.GenerateEventID(ids)
	require.NoError(t, err)
	corrID, err := id.GenerateCorrelationID(ids)
	require.NoError(t, err)
	in := runtimeorder.Intent{IntentID: intentID, Instrument: instID, Metadata: id.Metadata{EventID: eventID, CorrelationID: corrID}}
	mutate(&in)
	in, err = runtimeorder.NewIntent(in)
	require.NoError(t, err)
	return in
}

// TestPipeline_NothingToDoIsErrNoAction (ADR-067): intents the account
// already satisfies are classified ErrNoAction, wrapping the execution
// reason, with an empty Result and nothing submitted.
func TestPipeline_NothingToDoIsErrNoAction(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)
	inst := h.listing.InstrumentID()

	cases := []struct {
		name   string
		intent runtimeorder.Intent
		reason error
	}{
		{"exit with no position", mustKindIntent(t, h.ids, inst, func(in *runtimeorder.Intent) { in.Kind = order.IntentExit }), execution.ErrNoPositionToExit},
		{"stop with no position", mustAdjustStopIntent(t, h.ids, inst), execution.ErrNoPositionToProtect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := p.Submit(ctx, pipeline.Input{Intent: tc.intent, Listing: h.listing, Account: h.snapshot(t, ctx)})
			require.ErrorIs(t, err, pipeline.ErrNoAction)
			assert.ErrorIs(t, err, tc.reason, "the execution reason is preserved")
			assert.NotErrorIs(t, err, pipeline.ErrRejected)
			assert.Equal(t, pipeline.Result{}, result)
			assert.Empty(t, h.snapshot(t, ctx).OpenOrders(), "nothing reached the broker")
		})
	}
}

func TestPipeline_TargetAlreadyHeldIsErrNoAction(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "10000")
	p := newPipeline(t, h)
	target := func() runtimeorder.Intent {
		return mustKindIntent(t, h.ids, h.listing.InstrumentID(), func(in *runtimeorder.Intent) {
			in.Kind = order.IntentTargetExposure
			in.Side = order.Buy
			q := num.MustParseQuantity("100")
			in.Quantity = &q
		})
	}

	_, err := p.Submit(ctx, pipeline.Input{Intent: target(), Listing: h.listing, Account: h.snapshot(t, ctx)})
	require.NoError(t, err, "the first target opens 100 long")

	_, err = p.Submit(ctx, pipeline.Input{Intent: target(), Listing: h.listing, Account: h.snapshot(t, ctx)})
	require.ErrorIs(t, err, pipeline.ErrNoAction)
	assert.ErrorIs(t, err, execution.ErrAlreadyAtTarget)
}
