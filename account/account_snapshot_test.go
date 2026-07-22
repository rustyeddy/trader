package account

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── mock ─────────────────────────────────────────────────────────────────────

type mockAccountPoller struct {
	mu      sync.Mutex
	details *oanda.AccountDetails
	changes []*oanda.AccountChangesResult
	idx     int
	// pollCh receives the sinceID on each GetAccountChanges call.
	pollCh chan int64
}

func (m *mockAccountPoller) GetAccountDetails(_ context.Context, _ string) (*oanda.AccountDetails, error) {
	return m.details, nil
}

func (m *mockAccountPoller) GetAccountChanges(_ context.Context, _ string, sinceID int64) (*oanda.AccountChangesResult, error) {
	if m.pollCh != nil {
		m.pollCh <- sinceID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idx >= len(m.changes) {
		return &oanda.AccountChangesResult{
			TradesReduced: map[string]int64{},
			TradeState:    map[string]float64{},
		}, nil
	}
	r := m.changes[m.idx]
	m.idx++
	return r, nil
}

// ─── tests ────────────────────────────────────────────────────────────────────

func TestAccountSnapshot_InitialState(t *testing.T) {
	mock := &mockAccountPoller{
		details: &oanda.AccountDetails{
			AccountSummary: oanda.AccountSummary{
				ID:      "acc-1",
				Balance: 10000,
				NAV:     10150,
			},
			OpenTrades: []oanda.OpenTrade{
				{ID: "7", Instrument: "EUR_USD", Units: 1000, EntryPrice: 1.085},
			},
			LastTransactionID: 42,
		},
	}

	snap := newAccountSnapshot(mock, "acc-1", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := snap.Start(ctx, time.Hour) // long interval — won't fire in test
	require.NoError(t, err)
	assert.True(t, snap.IsRunning())

	assert.InDelta(t, 10000.0, snap.Balance(), 0.001)
	assert.InDelta(t, 10150.0, snap.NAV(), 0.001)

	trades := snap.OpenTrades()
	require.Len(t, trades, 1)
	assert.Equal(t, "7", trades[0].ID)
}

func TestAccountSnapshot_StartIdempotent(t *testing.T) {
	mock := &mockAccountPoller{
		details: &oanda.AccountDetails{
			AccountSummary: oanda.AccountSummary{NAV: 5000},
		},
	}
	snap := newAccountSnapshot(mock, "acc-1", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, snap.Start(ctx, time.Hour))
	require.NoError(t, snap.Start(ctx, time.Hour)) // second call is a no-op
	assert.True(t, snap.IsRunning())
}

func TestAccountSnapshot_ApplyChanges_TradeLifecycle(t *testing.T) {
	mock := &mockAccountPoller{
		details: &oanda.AccountDetails{
			AccountSummary: oanda.AccountSummary{NAV: 10000, Balance: 10000},
			OpenTrades: []oanda.OpenTrade{
				{ID: "7", Instrument: "EUR_USD", Units: 1000},
			},
			LastTransactionID: 10,
		},
		changes: []*oanda.AccountChangesResult{
			{
				// A new trade opens; trade 7 gets a partial close; NAV updates.
				TradesOpened:  []oanda.OpenTrade{{ID: "11", Instrument: "GBP_USD", Units: 500}},
				TradesReduced: map[string]int64{"7": 500},
				TradeState: map[string]float64{
					"7":  25.0,
					"11": 10.0,
				},
				NAV:               10035.0,
				UnrealizedPL:      35.0,
				LastTransactionID: 20,
			},
			{
				// Trade 7 fully closes; balance updated from fill.
				TradesClosed:      []string{"7"},
				TradesReduced:     map[string]int64{},
				TradeState:        map[string]float64{"11": 15.0},
				NAV:               10030.0,
				BalanceAfterFill:  10020.0,
				LastTransactionID: 30,
			},
		},
	}

	pollCh := make(chan int64, 4)
	mock.pollCh = pollCh

	snap := newAccountSnapshot(mock, "acc-1", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, snap.Start(ctx, 10*time.Millisecond))

	// Wait for both change polls to be applied.
	<-pollCh // first poll
	<-pollCh // second poll
	// Give applyChanges a moment to finish.
	time.Sleep(5 * time.Millisecond)
	cancel()

	trades := snap.OpenTrades()
	require.Len(t, trades, 1, "only GBP_USD should remain")
	assert.Equal(t, "11", trades[0].ID)
	assert.InDelta(t, 15.0, trades[0].UnrealizedPL, 0.001)

	assert.InDelta(t, 10020.0, snap.Balance(), 0.001)
	assert.InDelta(t, 10030.0, snap.NAV(), 0.001)
}

func TestAccountSnapshot_StopsWhenContextCancelled(t *testing.T) {
	mock := &mockAccountPoller{
		details: &oanda.AccountDetails{
			AccountSummary: oanda.AccountSummary{NAV: 1000},
		},
	}

	snap := newAccountSnapshot(mock, "acc-1", nil)
	ctx, cancel := context.WithCancel(context.Background())

	require.NoError(t, snap.Start(ctx, 10*time.Millisecond))
	assert.True(t, snap.IsRunning())

	cancel()
	// Allow goroutine to observe cancellation and set started=false.
	require.Eventually(t, func() bool {
		return !snap.IsRunning()
	}, time.Second, 5*time.Millisecond, "snapshot should stop after context cancel")
}

func TestAccountSnapshot_StopsWithoutContextCancel(t *testing.T) {
	mock := &mockAccountPoller{
		details: &oanda.AccountDetails{
			AccountSummary: oanda.AccountSummary{NAV: 1000},
		},
	}

	snap := newAccountSnapshot(mock, "acc-1", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, snap.Start(ctx, 10*time.Millisecond))
	assert.True(t, snap.IsRunning())

	require.NoError(t, snap.Stop())
	// Stop() alone, without cancelling the external ctx, must still halt
	// the poll loop.
	require.Eventually(t, func() bool {
		return !snap.IsRunning()
	}, time.Second, 5*time.Millisecond, "snapshot should stop after Stop()")
}

func TestAccountSnapshot_StopBeforeStartIsNoop(t *testing.T) {
	snap := newAccountSnapshot(&mockAccountPoller{}, "acc-1", nil)
	assert.NotPanics(t, func() {
		assert.NoError(t, snap.Stop())
	})
}

func TestAccountSnapshot_Summary(t *testing.T) {
	mock := &mockAccountPoller{
		details: &oanda.AccountDetails{
			AccountSummary: oanda.AccountSummary{
				ID:           "acc-42",
				Balance:      9000,
				NAV:          9100,
				UnrealizedPL: 100,
				MarginUsed:   50,
				MarginAvail:  9050,
			},
		},
	}
	snap := newAccountSnapshot(mock, "acc-42", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, snap.Start(ctx, time.Hour))

	s := snap.Summary()
	assert.Equal(t, "acc-42", s.ID)
	assert.InDelta(t, 9000.0, s.Balance, 0.001)
	assert.InDelta(t, 9100.0, s.NAV, 0.001)
}
