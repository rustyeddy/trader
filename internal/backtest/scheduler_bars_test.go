package backtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/backtest"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// snapshotStrategy is a pure snapshot consumer (strategy.BarsHandler):
// it records every BarsEvent, optionally emits Enter intents for the
// instruments in enter on its first snapshot, and fails the test if the
// scheduler ever calls OnBar on it.
type snapshotStrategy struct {
	t            *testing.T
	requirements []strategy.DataRequirement
	enter        []instrument.ID
	err          error

	events  []strategy.BarsEvent
	intents strategy.IntentFactory
}

func (s *snapshotStrategy) Describe() strategy.Descriptor {
	return strategy.Descriptor{Name: "snapshot", Version: "test", Requirements: s.requirements}
}

func (s *snapshotStrategy) Start(_ context.Context, env strategy.Environment) error {
	s.intents = env.Intents
	return nil
}

func (s *snapshotStrategy) OnBar(context.Context, strategy.BarEvent, strategy.View) ([]runtimeorder.Intent, error) {
	s.t.Error("scheduler called OnBar on a BarsHandler strategy")
	return nil, nil
}

func (s *snapshotStrategy) OnBars(_ context.Context, ev strategy.BarsEvent, _ strategy.View) ([]runtimeorder.Intent, error) {
	s.events = append(s.events, ev)
	if s.err != nil {
		return nil, s.err
	}
	if len(s.events) != 1 {
		return nil, nil
	}
	var out []runtimeorder.Intent
	for _, id := range s.enter {
		in, err := s.intents.Enter(id, order.Buy)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

var _ strategy.BarsHandler = (*snapshotStrategy)(nil)

func audusdID() instrument.ID {
	return instrument.CurrencyPairID(num.MustParseCurrency("AUD"), num.MustParseCurrency("USD"))
}

func runSnapshot(t *testing.T, strat *snapshotStrategy) (schedulerHarness, error) {
	t.Helper()
	mgr := newSchedulerTestManager(t)
	replay := newTwoInstrumentReplay(t, mgr)
	t.Cleanup(func() { _ = replay.Close() })

	h := newSchedulerHarness(t, schedulerSpan(t).Start())
	deps := newSchedulerDeps(t, replay, strat, h)
	sched, err := backtest.NewScheduler(deps)
	if err != nil {
		return h, err
	}
	return h, sched.Run(context.Background())
}

func TestScheduler_BarsHandlerGetsOneSnapshotPerBoundary(t *testing.T) {
	strat := &snapshotStrategy{t: t, requirements: bothInstrumentsRequirements(t)}
	_, err := runSnapshot(t, strat)
	require.NoError(t, err)

	// schedulerSpan yields 4 H1 boundaries, each carrying both instruments.
	require.Len(t, strat.events, 4)
	for i, ev := range strat.events {
		require.Equal(t, marketdata.H1, ev.Interval)
		require.Empty(t, ev.Missing)
		require.Len(t, ev.Bars, 2)
		// Requirement-declaration order, not replay order.
		require.True(t, ev.Bars[0].Instrument.Equal(eurusdID(t)))
		require.True(t, ev.Bars[1].Instrument.Equal(gbpusdID(t)))
		for _, b := range ev.Bars {
			require.True(t, b.Bar.Time.Equal(ev.Boundary), "every member shares the snapshot boundary")
		}
		if i > 0 {
			require.True(t, ev.Boundary.After(strat.events[i-1].Boundary))
		}
	}
}

func TestScheduler_BarsHandlerReportsMissingMembersExplicitly(t *testing.T) {
	reqs := append(bothInstrumentsRequirements(t), strategy.DataRequirement{Instrument: audusdID(), Interval: marketdata.H1})
	strat := &snapshotStrategy{t: t, requirements: reqs}
	_, err := runSnapshot(t, strat)
	require.NoError(t, err)

	require.Len(t, strat.events, 4)
	for _, ev := range strat.events {
		require.Len(t, ev.Bars, 2)
		require.Len(t, ev.Missing, 1, "a declared requirement with no bar must be listed, not silently dropped")
		require.True(t, ev.Missing[0].Equal(audusdID()))
	}
}

func TestScheduler_BarsHandlerIntentsFillAtNextBarOpen(t *testing.T) {
	strat := &snapshotStrategy{t: t, requirements: bothInstrumentsRequirements(t), enter: []instrument.ID{eurusdID(t), gbpusdID(t)}}
	h, err := runSnapshot(t, strat)
	require.NoError(t, err)

	acc, err := h.broker.OpenAccount(context.Background(), h.accountID)
	require.NoError(t, err)
	snap, err := acc.Snapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 2, "intents from the first snapshot become eligible on each instrument's next bar")
}

func TestScheduler_BarsHandlerIntentForUndeclaredInstrumentFails(t *testing.T) {
	strat := &snapshotStrategy{t: t, requirements: bothInstrumentsRequirements(t), enter: []instrument.ID{audusdID()}}
	_, err := runSnapshot(t, strat)
	require.ErrorIs(t, err, backtest.ErrInvalidSchedulerDeps)
}

func TestScheduler_BarsHandlerCallbackErrorAbortsRun(t *testing.T) {
	boom := errors.New("scan failed")
	strat := &snapshotStrategy{t: t, requirements: bothInstrumentsRequirements(t), err: boom}
	_, err := runSnapshot(t, strat)
	require.ErrorIs(t, err, boom)
	require.Len(t, strat.events, 1)
}

func TestScheduler_BarsHandlerRequiresSingleInterval(t *testing.T) {
	strat := &snapshotStrategy{t: t, requirements: []strategy.DataRequirement{
		{Instrument: eurusdID(t), Interval: marketdata.H1},
		{Instrument: gbpusdID(t), Interval: marketdata.D1},
	}}
	_, err := runSnapshot(t, strat)
	require.ErrorIs(t, err, backtest.ErrInvalidSchedulerDeps)
	require.ErrorContains(t, err, "single interval")
}
