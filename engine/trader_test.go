package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/idgen"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraderProcessEventValidation(t *testing.T) {
	t.Parallel()

	tr := &Trader{}

	require.ErrorContains(t, tr.processEvent(context.Background(), nil), "nil broker event")

	require.ErrorContains(t,
		tr.processEvent(context.Background(), &execution.Event{Type: execution.EventOrderFilled}),
		"no position")

	require.ErrorContains(t,
		tr.processEvent(context.Background(), &execution.Event{Type: execution.EventPositionClosed}),
		"missing position")

	require.ErrorContains(t,
		tr.processEvent(context.Background(), &execution.Event{Type: execution.EventPositionClosed, Lot: &execution.Lot{TradeCommon: &execution.TradeCommon{ID: idgen.NewULID()}}}),
		"missing trade")

	require.NoError(t,
		tr.processEvent(context.Background(), &execution.Event{Type: execution.EventOrderFilled, Lot: &execution.Lot{TradeCommon: &execution.TradeCommon{ID: idgen.NewULID()}}}))

	require.NoError(t,
		tr.processEvent(context.Background(), &execution.Event{
			Type:  execution.EventPositionClosed,
			Lot:   &execution.Lot{TradeCommon: &execution.TradeCommon{ID: idgen.NewULID()}},
			Trade: &execution.Trade{TradeCommon: &execution.TradeCommon{ID: idgen.NewULID()}},
		}))

	// Unsupported event types are intentionally non-fatal.
	require.NoError(t, tr.processEvent(context.Background(), &execution.Event{Type: execution.EventType(255)}))
}

func TestTraderStartBrokerEventHandler_ProcessesAndPropagatesError(t *testing.T) {
	t.Parallel()

	tr := &Trader{}
	evtQ := make(chan *execution.Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var processed int64
	errCh, done := tr.StartBrokerEventHandler(ctx, evtQ, &processed)

	evtQ <- &execution.Event{Type: execution.EventOrderFilled, Lot: &execution.Lot{TradeCommon: &execution.TradeCommon{ID: idgen.NewULID()}}}
	assert.Eventually(t, func() bool {
		return atomic.LoadInt64(&processed) == 1
	}, 200*time.Millisecond, 5*time.Millisecond)

	evtQ <- &execution.Event{Type: execution.EventOrderFilled} // triggers processEvent error

	select {
	case err := <-errCh:
		require.ErrorContains(t, err, "no position")
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected handler error")
	}

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected handler goroutine to stop")
	}
}

func TestTraderBrokerEventErrorAndWaitForBrokerIdle(t *testing.T) {
	t.Parallel()

	tr := &Trader{}
	errCh := make(chan error, 1)
	require.NoError(t, tr.BrokerEventError(errCh))

	errCh <- errors.New("boom")
	require.EqualError(t, tr.BrokerEventError(errCh), "boom")

	b := execution.NewBroker("idle")
	b.Account = execution.NewAccount("acct", types.MoneyFromFloat(10_000))
	idle := &Trader{Broker: b}
	require.NoError(t, idle.WaitForBrokerIdle(make(chan error, 1), 5*time.Millisecond))

	bad := make(chan error, 1)
	bad <- errors.New("from broker")
	require.EqualError(t, idle.WaitForBrokerIdle(bad, 5*time.Millisecond), "from broker")

	require.True(t, b.EnqueueEvent(&execution.Event{Type: execution.EventOrderFilled, Lot: &execution.Lot{TradeCommon: &execution.TradeCommon{ID: idgen.NewULID()}}}))
	require.ErrorContains(t, idle.WaitForBrokerIdle(make(chan error, 1), 5*time.Millisecond), "broker did not become idle")
}

func TestSnapshotLots_FiltersByState(t *testing.T) {
	t.Parallel()

	src := &execution.LotBook{}
	src.Add(&execution.Lot{TradeCommon: &execution.TradeCommon{ID: "open"}, State: execution.LotOpen})
	src.Add(&execution.Lot{TradeCommon: &execution.TradeCommon{ID: "open-req"}, State: execution.LotOpenRequested})
	src.Add(&execution.Lot{TradeCommon: &execution.TradeCommon{ID: "close-req"}, State: execution.LotCloseRequested})
	src.Add(&execution.Lot{TradeCommon: &execution.TradeCommon{ID: "closed"}, State: execution.LotClosed})

	got := SnapshotLots(src)
	require.NotNil(t, got)
	lots := got.All()
	require.Len(t, lots, 3)
	assert.Contains(t, lots, "open")
	assert.Contains(t, lots, "open-req")
	assert.Contains(t, lots, "close-req")
	assert.NotContains(t, lots, "closed")
}
