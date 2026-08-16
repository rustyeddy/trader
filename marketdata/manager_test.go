package marketdata

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider and fakeStore are in-package fakes for Manager's internal
// collaborator seams. They let the Manager tests exercise internal wiring
// without any filesystem or network dependency.
type fakeProvider struct{ id string }

func (f fakeProvider) name() string { return f.id }

type fakeStore struct{ dir string }

func (f fakeStore) root() string { return f.dir }
func (f fakeStore) publish(context.Context, partitionKey, Manifest, BarSet) error {
	return errors.New("fakeStore: publish not implemented")
}
func (f fakeStore) load(context.Context, partitionKey) (Manifest, BarSet, error) {
	return Manifest{}, BarSet{}, errors.New("fakeStore: load not implemented")
}

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

func TestNewManagerWithInternalCollaborators(t *testing.T) {
	m, err := New(Config{
		Clock:     testClock(),
		StoreRoot: "/data/candles",
		provider:  fakeProvider{id: "fake"},
		store:     fakeStore{dir: "/data/candles"},
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
// through New) is reported as not configured rather than being allowed to
// misbehave. New is the only way to obtain a usable Manager.
func TestZeroValueManagerNotConfigured(t *testing.T) {
	var m Manager
	assert.False(t, m.configured())
}

// A nil *Manager is a plausible caller mistake; configured must treat it as
// unconfigured rather than panic.
func TestNilManagerNotConfigured(t *testing.T) {
	var m *Manager
	assert.False(t, m.configured())
}
