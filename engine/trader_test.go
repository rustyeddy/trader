package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
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
		tr.processEvent(context.Background(), &account.Event{Type: account.EventOrderFilled}),
		"no position")

	require.ErrorContains(t,
		tr.processEvent(context.Background(), &account.Event{Type: account.EventPositionClosed}),
		"missing position")

	require.ErrorContains(t,
		tr.processEvent(context.Background(), &account.Event{Type: account.EventPositionClosed, Lot: &account.Lot{TradeCommon: &account.TradeCommon{ID: idgen.NewULID()}}}),
		"missing trade")

	require.NoError(t,
		tr.processEvent(context.Background(), &account.Event{Type: account.EventOrderFilled, Lot: &account.Lot{TradeCommon: &account.TradeCommon{ID: idgen.NewULID()}}}))

	require.NoError(t,
		tr.processEvent(context.Background(), &account.Event{
			Type:  account.EventPositionClosed,
			Lot:   &account.Lot{TradeCommon: &account.TradeCommon{ID: idgen.NewULID()}},
			Trade: &account.Trade{TradeCommon: &account.TradeCommon{ID: idgen.NewULID()}},
		}))

	// Unsupported event types are intentionally non-fatal.
	require.NoError(t, tr.processEvent(context.Background(), &account.Event{Type: account.EventType(255)}))
}

func TestTraderStartBrokerEventHandler_ProcessesAndPropagatesError(t *testing.T) {
	t.Parallel()

	tr := &Trader{}
	evtQ := make(chan *account.Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var processed int64
	errCh, done := tr.StartBrokerEventHandler(ctx, evtQ, &processed)

	evtQ <- &account.Event{Type: account.EventOrderFilled, Lot: &account.Lot{TradeCommon: &account.TradeCommon{ID: idgen.NewULID()}}}
	assert.Eventually(t, func() bool {
		return atomic.LoadInt64(&processed) == 1
	}, 200*time.Millisecond, 5*time.Millisecond)

	evtQ <- &account.Event{Type: account.EventOrderFilled} // triggers processEvent error

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

	b := account.NewBroker("idle")
	b.Account = account.NewAccount("acct", types.MoneyFromFloat(10_000))
	idle := &Trader{Broker: b}
	require.NoError(t, idle.WaitForBrokerIdle(make(chan error, 1), 5*time.Millisecond))

	bad := make(chan error, 1)
	bad <- errors.New("from broker")
	require.EqualError(t, idle.WaitForBrokerIdle(bad, 5*time.Millisecond), "from broker")

	require.True(t, b.EnqueueEvent(&account.Event{Type: account.EventOrderFilled, Lot: &account.Lot{TradeCommon: &account.TradeCommon{ID: idgen.NewULID()}}}))
	require.ErrorContains(t, idle.WaitForBrokerIdle(make(chan error, 1), 5*time.Millisecond), "broker did not become idle")
}

func TestSnapshotLots_FiltersByState(t *testing.T) {
	t.Parallel()

	src := &account.LotBook{}
	src.Add(&account.Lot{TradeCommon: &account.TradeCommon{ID: "open"}, State: account.LotOpen})
	src.Add(&account.Lot{TradeCommon: &account.TradeCommon{ID: "open-req"}, State: account.LotOpenRequested})
	src.Add(&account.Lot{TradeCommon: &account.TradeCommon{ID: "close-req"}, State: account.LotCloseRequested})
	src.Add(&account.Lot{TradeCommon: &account.TradeCommon{ID: "closed"}, State: account.LotClosed})

	got := SnapshotLots(src)
	require.NotNil(t, got)
	lots := got.All()
	require.Len(t, lots, 3)
	assert.Contains(t, lots, "open")
	assert.Contains(t, lots, "open-req")
	assert.Contains(t, lots, "close-req")
	assert.NotContains(t, lots, "closed")
}
