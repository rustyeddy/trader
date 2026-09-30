package backtest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/backtest"
	"github.com/rustyeddy/trader/internal/journal"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// TestScheduler_IntentWithNothingToDoIsJournaledNoAction (ADR-067): an
// exit or protective stop on a flat account is an expected no-op. The
// run completes, and each such intent is followed by a KindNoAction
// record correlated to it, never by a proposal.
func TestScheduler_IntentWithNothingToDoIsJournaledNoAction(t *testing.T) {
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	eur := eurusdID(t)
	strat := &recordingStrategy{
		requirements: bothInstrumentsRequirements(t),
		emit: func(f strategy.IntentFactory, ev strategy.BarEvent) ([]runtimeorder.Intent, error) {
			if !ev.Instrument.Equal(eur) {
				return nil, nil
			}
			exit, err := f.Exit(ev.Instrument)
			if err != nil {
				return nil, err
			}
			stop, err := f.AdjustStop(ev.Instrument, num.MustParsePrice("1.00000"))
			if err != nil {
				return nil, err
			}
			return []runtimeorder.Intent{exit, stop}, nil
		},
	}
	deps := newSchedulerDeps(t, replay, strat, h)
	rec := &capturingRecorder{}
	deps.Journal = rec

	sched, err := backtest.NewScheduler(deps)
	require.NoError(t, err)
	require.NoError(t, sched.Run(context.Background()), "a no-op intent must not abort the run")

	intents := map[string]runtimeorder.Intent{}
	var noActions []journal.Record
	for _, r := range rec.all() {
		switch r.Kind {
		case journal.KindIntent:
			intents[r.Intent.IntentID.String()] = *r.Intent
		case journal.KindNoAction:
			noActions = append(noActions, r)
		case journal.KindProposal, journal.KindRequest, journal.KindOrder:
			t.Fatalf("a no-op intent must produce no %s record", r.Kind)
		}
	}
	require.NotEmpty(t, noActions)
	assert.Len(t, noActions, len(intents), "every intent was a no-op")

	var sawExit, sawStop bool
	for _, r := range noActions {
		in, ok := intents[r.NoAction.IntentID.String()]
		require.True(t, ok, "the no-action names a journaled intent")
		assert.Equal(t, in.Metadata.CorrelationID, r.Metadata.CorrelationID, "correlated to its intent")
		switch in.Kind {
		case order.IntentExit:
			sawExit = true
			assert.Contains(t, r.NoAction.Reason, "no open position to exit")
		case order.IntentAdjustStop:
			sawStop = true
			assert.Contains(t, r.NoAction.Reason, "no open position to protect")
		}
	}
	assert.True(t, sawExit)
	assert.True(t, sawStop)
}
