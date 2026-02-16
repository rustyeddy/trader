package backtest

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/broker/sim"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTickFeed is a simple in-memory feed for testing
type mockTickFeed struct {
	ticks  []market.Tick
	index  int
	closed bool
}

func newMockTickFeed(ticks []market.Tick) *mockTickFeed {
	return &mockTickFeed{ticks: ticks}
}

func (m *mockTickFeed) Next() (market.Tick, bool, error) {
	if m.index >= len(m.ticks) {
		return market.Tick{}, false, nil
	}
	p := m.ticks[m.index]
	m.index++
	return p, true, nil
}

func (m *mockTickFeed) Close() error {
	m.closed = true
	return nil
}

// errorTickFeed returns an error on Next()
type errorTickFeed struct{}

func (e *errorTickFeed) Next() (market.Tick, bool, error) {
	return market.Tick{}, false, errors.New("mock error")
}

func (e *errorTickFeed) Close() error {
	return nil
}

// mockStrategy tracks OnTick calls
type mockStrategy struct {
	tickCount int
	shouldErr bool
}

func (m *mockStrategy) OnTick(ctx context.Context, b broker.Broker, p market.Tick) error {
	m.tickCount++
	if m.shouldErr {
		return errors.New("strategy error")
	}
	return nil
}

func TestRunner_Run_Validation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("missing engine", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			Engine:   nil,
			Feed:     newMockTickFeed(nil),
			Strategy: &mockStrategy{},
		}

		_, err := r.Run(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, "backtest: Engine is required", err.Error())
	})

	t.Run("missing feed", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		dbPath := filepath.Join(tmp, "test.sqlite")
		j, err := journal.NewSQLite(dbPath)
		require.NoError(t, err)
		defer j.Close()

		engine := sim.NewEngine(broker.Account{
			ID:       "TEST",
			Currency: "USD",
			Balance:  10000,
			Equity:   10000,
		}, j)

		r := &Runner{
			Engine:   engine,
			Feed:     nil,
			Strategy: &mockStrategy{},
		}

		_, err = r.Run(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, "backtest: Feed is required", err.Error())
	})

	t.Run("missing strategy", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		dbPath := filepath.Join(tmp, "test.sqlite")
		j, err := journal.NewSQLite(dbPath)
		require.NoError(t, err)
		defer j.Close()

		engine := sim.NewEngine(broker.Account{
			ID:       "TEST",
			Currency: "USD",
			Balance:  10000,
			Equity:   10000,
		}, j)

		r := &Runner{
			Engine:   engine,
			Feed:     newMockTickFeed(nil),
			Strategy: nil,
		}

		_, err = r.Run(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, "backtest: Strategy is required", err.Error())
	})
}

func TestRunner_Run_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	require.NoError(t, err)
	defer j.Close()

	startBal := 10000.0
	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  startBal,
		Equity:   startBal,
	}, j)

	ticks := []market.Tick{
		{
			Time:       time.Date(2026, 1, 24, 9, 30, 0, 0, time.UTC),
			Instrument: "EUR_USD",
			Bid:        1.1000,
			Ask:        1.1002,
		},
		{
			Time:       time.Date(2026, 1, 24, 9, 30, 5, 0, time.UTC),
			Instrument: "EUR_USD",
			Bid:        1.1010,
			Ask:        1.1012,
		},
		{
			Time:       time.Date(2026, 1, 24, 9, 30, 10, 0, time.UTC),
			Instrument: "EUR_USD",
			Bid:        1.1020,
			Ask:        1.1022,
		},
	}

	feed := newMockTickFeed(ticks)
	strategy := &mockStrategy{}

	r := &Runner{
		Engine:   engine,
		Feed:     feed,
		Strategy: strategy,
	}

	result, err := r.Run(ctx, j)
	require.NoError(t, err)

	// Verify feed was closed
	assert.True(t, feed.closed, "expected feed to be closed")

	// Verify strategy received all ticks
	assert.Equal(t, len(ticks), strategy.tickCount)

	// Verify result
	assert.Equal(t, startBal, result.Balance)

	expectedStart := time.Date(2026, 1, 24, 9, 30, 0, 0, time.UTC)
	expectedEnd := time.Date(2026, 1, 24, 9, 30, 10, 0, time.UTC)

	assert.True(t, result.Start.Equal(expectedStart))
	assert.True(t, result.End.Equal(expectedEnd))
}

func TestRunner_Run_EmptyFeed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	require.NoError(t, err)
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  10000,
		Equity:   10000,
	}, j)

	feed := newMockTickFeed(nil)
	strategy := &mockStrategy{}

	r := &Runner{
		Engine:   engine,
		Feed:     feed,
		Strategy: strategy,
	}

	result, err := r.Run(ctx, j)
	require.NoError(t, err)

	// No ticks means no strategy calls
	assert.Equal(t, 0, strategy.tickCount)

	// Start and End should be zero
	assert.True(t, result.Start.IsZero())
	assert.True(t, result.End.IsZero())
}

func TestRunner_Run_FeedError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	require.NoError(t, err)
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  10000,
		Equity:   10000,
	}, j)

	feed := &errorTickFeed{}
	strategy := &mockStrategy{}

	r := &Runner{
		Engine:   engine,
		Feed:     feed,
		Strategy: strategy,
	}

	_, err = r.Run(ctx, j)
	assert.Error(t, err)
}

func TestRunner_Run_StrategyError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	require.NoError(t, err)
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  10000,
		Equity:   10000,
	}, j)

	ticks := []market.Tick{
		{
			Time:       time.Date(2026, 1, 24, 9, 30, 0, 0, time.UTC),
			Instrument: "EUR_USD",
			Bid:        1.1000,
			Ask:        1.1002,
		},
	}

	feed := newMockTickFeed(ticks)
	strategy := &mockStrategy{shouldErr: true}

	r := &Runner{
		Engine:   engine,
		Feed:     feed,
		Strategy: strategy,
	}

	_, err = r.Run(ctx, j)
	assert.Error(t, err)
}

func TestRunner_Run_CloseEnd(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	require.NoError(t, err)
	defer j.Close()

	startBal := 10000.0
	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  startBal,
		Equity:   startBal,
	}, j)

	ticks := []market.Tick{
		{
			Time:       time.Date(2026, 1, 24, 9, 30, 0, 0, time.UTC),
			Instrument: "EUR_USD",
			Bid:        1.1000,
			Ask:        1.1002,
		},
	}

	feed := newMockTickFeed(ticks)
	strategy := &mockStrategy{}

	r := &Runner{
		Engine:   engine,
		Feed:     feed,
		Strategy: strategy,
		Options: RunnerOptions{
			CloseEnd:    true,
			CloseReason: "TestEnd",
		},
	}

	_, err = r.Run(ctx, j)
	require.NoError(t, err)

	// Test passes if CloseAll doesn't panic or error
	// The actual behavior of CloseAll is tested in the engine tests
}

func TestRunner_Run_CloseEndDefaultReason(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	require.NoError(t, err)
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  10000,
		Equity:   10000,
	}, j)

	ticks := []market.Tick{
		{
			Time:       time.Date(2026, 1, 24, 9, 30, 0, 0, time.UTC),
			Instrument: "EUR_USD",
			Bid:        1.1000,
			Ask:        1.1002,
		},
	}

	feed := newMockTickFeed(ticks)
	strategy := &mockStrategy{}

	r := &Runner{
		Engine:   engine,
		Feed:     feed,
		Strategy: strategy,
		Options: RunnerOptions{
			CloseEnd:    true,
			CloseReason: "", // Empty should use default "EndOfReplay"
		},
	}

	_, err = r.Run(ctx, j)
	require.NoError(t, err)
}

func TestRunner_Run_WithoutJournal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	require.NoError(t, err)
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  10000,
		Equity:   10000,
	}, j)

	ticks := []market.Tick{
		{
			Time:       time.Date(2026, 1, 24, 9, 30, 0, 0, time.UTC),
			Instrument: "EUR_USD",
			Bid:        1.1000,
			Ask:        1.1002,
		},
	}

	feed := newMockTickFeed(ticks)
	strategy := &mockStrategy{}

	r := &Runner{
		Engine:   engine,
		Feed:     feed,
		Strategy: strategy,
	}

	// Run without journal (nil)
	result, err := r.Run(ctx, nil)
	require.NoError(t, err)

	// Trade counts should be zero when no journal is provided
	assert.Equal(t, 0, result.Trades)
	assert.Equal(t, 0, result.Wins)
	assert.Equal(t, 0, result.Losses)
}
