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
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    "/data/candles",
		Resolver:     testResolver(t),
		ProviderName: "oanda",
	})
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.True(t, m.configured())
}

func TestNewManagerDefaultsCalendarWhenUnset(t *testing.T) {
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    "/data/candles",
		Resolver:     testResolver(t),
		ProviderName: "oanda",
	})
	require.NoError(t, err)
	require.NotNil(t, m.calendar)
	if _, ok := m.calendar.(*FXCalendar); !ok {
		t.Fatalf("expected *FXCalendar default, got %T", m.calendar)
	}
}

func TestNewManagerHonorsExplicitCalendar(t *testing.T) {
	cal := NewFXCalendar(FXCalendarParams{Holidays: []time.Time{time.Date(2024, 12, 25, 0, 0, 0, 0, time.UTC)}})
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    "/data/candles",
		Resolver:     testResolver(t),
		ProviderName: "oanda",
		Calendar:     cal,
	})
	require.NoError(t, err)
	assert.Same(t, cal, m.calendar)
}

func TestNewManagerRawRootOptional(t *testing.T) {
	// RawRoot is not required for construction: only Coverage/Plan need
	// it, and they check for it themselves when called.
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    "/data/candles",
		Resolver:     testResolver(t),
		ProviderName: "oanda",
	})
	require.NoError(t, err)
	assert.True(t, m.configured())
	assert.Empty(t, m.rawRoot)
}

func TestNewManagerBuildsRealStoreFromStoreRoot(t *testing.T) {
	// New must not leave m.store nil for real (non-test) construction:
	// nothing outside this package can ever set Config.store, so if New
	// didn't build one internally, Manager would be unusable in
	// production. This is what closes the gap the M2-03 skeleton left
	// open.
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    t.TempDir(),
		Resolver:     testResolver(t),
		ProviderName: "oanda",
	})
	require.NoError(t, err)
	require.NotNil(t, m.store)
	if _, ok := m.store.(*canonicalCSVStore); !ok {
		t.Fatalf("expected *canonicalCSVStore, got %T", m.store)
	}
}

func TestNewManagerWithInternalCollaborators(t *testing.T) {
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    "/data/candles",
		Resolver:     testResolver(t),
		ProviderName: "oanda",
		provider:     fakeProvider{id: "fake"},
		store:        fakeStore{dir: "/data/candles"},
	})
	require.NoError(t, err)
	assert.Equal(t, "fake", m.provider.name())
	assert.Equal(t, "/data/candles", m.store.root())
}

func TestNewManagerRejectsMissingClock(t *testing.T) {
	m, err := New(Config{StoreRoot: "/data/candles", Resolver: testResolver(t), ProviderName: "oanda"})
	assert.Nil(t, m)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewManagerRejectsEmptyStoreRoot(t *testing.T) {
	m, err := New(Config{Clock: testClock(), Resolver: testResolver(t), ProviderName: "oanda"})
	assert.Nil(t, m)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewManagerRejectsMissingResolver(t *testing.T) {
	m, err := New(Config{Clock: testClock(), StoreRoot: "/data/candles", ProviderName: "oanda"})
	assert.Nil(t, m)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewManagerRejectsEmptyProviderName(t *testing.T) {
	m, err := New(Config{Clock: testClock(), StoreRoot: "/data/candles", Resolver: testResolver(t)})
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
