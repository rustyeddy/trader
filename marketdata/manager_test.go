package marketdata

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider and fakeStore are in-package fakes for Manager's internal
// collaborator seams. They let the Manager tests exercise construction and
// injection without any filesystem or network dependency.
type fakeProvider struct{ id string }

func (f fakeProvider) name() string { return f.id }

type fakeStore struct{ dir string }

func (f fakeStore) root() string { return f.dir }

// testClock returns a deterministic clock for construction tests; no test
// here depends on wall-clock time.
func testClock() clock.Clock {
	return clock.NewSimulated(time.Date(2026, time.January, 7, 12, 0, 0, 0, time.UTC))
}

func TestNewManagerValid(t *testing.T) {
	m, err := New(Config{Clock: testClock(), StoreRoot: "/data/candles"})
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.True(t, m.configured())
}

func TestNewManagerWithCollaborators(t *testing.T) {
	m, err := New(Config{
		Clock:     testClock(),
		StoreRoot: "/data/candles",
		Provider:  fakeProvider{id: "fake"},
		Store:     fakeStore{dir: "/data/candles"},
	})
	require.NoError(t, err)
	assert.Equal(t, "fake", m.provider.name())
	assert.Equal(t, "/data/candles", m.store.root())
}

func TestNewManagerRejectsMissingClock(t *testing.T) {
	m, err := New(Config{StoreRoot: "/data/candles"})
	assert.Nil(t, m)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewManagerRejectsEmptyStoreRoot(t *testing.T) {
	m, err := New(Config{Clock: testClock()})
	assert.Nil(t, m)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

// A zero-value Manager (built by mistake as a struct literal rather than
// through New) must fail loudly and classifiably, not silently misbehave.
func TestZeroValueManagerNotConfigured(t *testing.T) {
	var m Manager
	assert.False(t, m.configured())

	err := m.Sync(context.Background())
	assert.ErrorIs(t, err, ErrNotConfigured)

	err = m.Build(context.Background())
	assert.ErrorIs(t, err, ErrNotConfigured)
}

// A nil *Manager is a plausible caller mistake; configured must treat it as
// unconfigured rather than panic.
func TestNilManagerNotConfigured(t *testing.T) {
	var m *Manager
	assert.False(t, m.configured())
}

func TestManagerOperationsReportNotImplemented(t *testing.T) {
	m, err := New(Config{Clock: testClock(), StoreRoot: "/data/candles"})
	require.NoError(t, err)

	assert.ErrorIs(t, m.Sync(context.Background()), ErrNotImplemented)
	assert.ErrorIs(t, m.Build(context.Background()), ErrNotImplemented)
}

// ErrNotImplemented must be distinguishable from ErrNotConfigured: a
// configured Manager's unbuilt operation is a different condition from an
// unconfigured Manager, and callers may branch on the two.
func TestNotImplementedIsNotNotConfigured(t *testing.T) {
	m, err := New(Config{Clock: testClock(), StoreRoot: "/data/candles"})
	require.NoError(t, err)

	err = m.Sync(context.Background())
	assert.ErrorIs(t, err, ErrNotImplemented)
	assert.NotErrorIs(t, err, ErrNotConfigured)
}

// Operations honor context cancellation before doing work, so a caller that
// cancels sees the cancellation rather than a masking not-implemented error.
func TestManagerOperationsHonorContextCancellation(t *testing.T) {
	m, err := New(Config{Clock: testClock(), StoreRoot: "/data/candles"})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.ErrorIs(t, m.Sync(ctx), context.Canceled)
	assert.ErrorIs(t, m.Build(ctx), context.Canceled)
}
