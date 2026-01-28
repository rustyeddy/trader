package backtest

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
)

// mockTickFeed is a simple in-memory feed for testing
type mockTickFeed struct {
	ticks  []broker.Price
	index  int
	closed bool
}

func newMockTickFeed(ticks []broker.Price) *mockTickFeed {
	return &mockTickFeed{ticks: ticks}
}

func (m *mockTickFeed) Next() (broker.Price, bool, error) {
	if m.index >= len(m.ticks) {
		return broker.Price{}, false, nil
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

func (e *errorTickFeed) Next() (broker.Price, bool, error) {
	return broker.Price{}, false, errors.New("mock error")
}

func (e *errorTickFeed) Close() error {
	return nil
}

// mockStrategy tracks OnTick calls
type mockStrategy struct {
	tickCount int
	shouldErr bool
}

func (m *mockStrategy) OnTick(ctx context.Context, b broker.Broker, p broker.Price) error {
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
		if err == nil {
			t.Error("expected error for missing Engine")
		}
		if err != nil && err.Error() != "backtest: Engine is required" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing feed", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		dbPath := filepath.Join(tmp, "test.sqlite")
		j, err := journal.NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("NewSQLite: %v", err)
		}
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
		if err == nil {
			t.Error("expected error for missing Feed")
		}
		if err != nil && err.Error() != "backtest: Feed is required" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing strategy", func(t *testing.T) {
		t.Parallel()

		tmp := t.TempDir()
		dbPath := filepath.Join(tmp, "test.sqlite")
		j, err := journal.NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("NewSQLite: %v", err)
		}
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
		if err == nil {
			t.Error("expected error for missing Strategy")
		}
		if err != nil && err.Error() != "backtest: Strategy is required" {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestRunner_Run_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer j.Close()

	startBal := 10000.0
	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  startBal,
		Equity:   startBal,
	}, j)

	ticks := []broker.Price{
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
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Verify feed was closed
	if !feed.closed {
		t.Error("expected feed to be closed")
	}

	// Verify strategy received all ticks
	if strategy.tickCount != len(ticks) {
		t.Errorf("strategy.tickCount = %d, want %d", strategy.tickCount, len(ticks))
	}

	// Verify result
	if result.Balance != startBal {
		t.Errorf("result.Balance = %v, want %v", result.Balance, startBal)
	}

	expectedStart := time.Date(2026, 1, 24, 9, 30, 0, 0, time.UTC)
	expectedEnd := time.Date(2026, 1, 24, 9, 30, 10, 0, time.UTC)

	if !result.Start.Equal(expectedStart) {
		t.Errorf("result.Start = %v, want %v", result.Start, expectedStart)
	}
	if !result.End.Equal(expectedEnd) {
		t.Errorf("result.End = %v, want %v", result.End, expectedEnd)
	}
}

func TestRunner_Run_EmptyFeed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
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
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// No ticks means no strategy calls
	if strategy.tickCount != 0 {
		t.Errorf("strategy.tickCount = %d, want 0", strategy.tickCount)
	}

	// Start and End should be zero
	if !result.Start.IsZero() {
		t.Errorf("result.Start = %v, want zero", result.Start)
	}
	if !result.End.IsZero() {
		t.Errorf("result.End = %v, want zero", result.End)
	}
}

func TestRunner_Run_FeedError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
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
	if err == nil {
		t.Error("expected error from feed")
	}
}

func TestRunner_Run_StrategyError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  10000,
		Equity:   10000,
	}, j)

	ticks := []broker.Price{
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
	if err == nil {
		t.Error("expected error from strategy")
	}
}

func TestRunner_Run_CloseEnd(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer j.Close()

	startBal := 10000.0
	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  startBal,
		Equity:   startBal,
	}, j)

	ticks := []broker.Price{
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
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Test passes if CloseAll doesn't panic or error
	// The actual behavior of CloseAll is tested in the engine tests
}

func TestRunner_Run_CloseEndDefaultReason(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  10000,
		Equity:   10000,
	}, j)

	ticks := []broker.Price{
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
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestRunner_Run_WithoutJournal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.sqlite")
	j, err := journal.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "TEST",
		Currency: "USD",
		Balance:  10000,
		Equity:   10000,
	}, j)

	ticks := []broker.Price{
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
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Trade counts should be zero when no journal is provided
	if result.Trades != 0 {
		t.Errorf("result.Trades = %d, want 0", result.Trades)
	}
	if result.Wins != 0 {
		t.Errorf("result.Wins = %d, want 0", result.Wins)
	}
	if result.Losses != 0 {
		t.Errorf("result.Losses = %d, want 0", result.Losses)
	}
}
