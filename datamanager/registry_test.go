package datamanager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider implements Provider with a fixed name and deterministic URL.
type stubProvider struct{ name string }

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) SourceURL(p SourceParams) string {
	return "https://stub/" + s.name + "/" + p.Instrument
}

// uniqueName returns a name that won't collide with other tests or
// pre-registered providers (e.g. dukascopy registered via init).
func uniqueName(t *testing.T) string {
	t.Helper()
	return "stub-" + t.Name()
}

// ── Register ──────────────────────────────────────────────────────────────────

func TestRegister_NilIsNoOp(t *testing.T) {
	Register(nil)
	_, err := Get("")
	assert.Error(t, err, "registering nil should not add an entry")
}

func TestRegister_AddsProvider(t *testing.T) {
	name := uniqueName(t)
	Register(&stubProvider{name: name})
	p, err := Get(name)
	require.NoError(t, err)
	assert.Equal(t, name, p.Name())
}

func TestRegister_OverwritesPreviousEntry(t *testing.T) {
	name := uniqueName(t)
	first := &stubProvider{name: name}
	second := &stubProvider{name: name}
	Register(first)
	Register(second) // re-register same name
	p, err := Get(name)
	require.NoError(t, err)
	assert.Same(t, second, p, "re-registering same name should replace the entry")
}

// ── Get ───────────────────────────────────────────────────────────────────────

func TestGet_UnknownNameReturnsError(t *testing.T) {
	_, err := Get("nonexistent-provider-xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent-provider-xyz")
}

func TestGet_ReturnsRegisteredProvider(t *testing.T) {
	name := uniqueName(t)
	Register(&stubProvider{name: name})
	p, err := Get(name)
	require.NoError(t, err)
	assert.Equal(t, name, p.Name())
}

// ── SourceURL via Provider interface ─────────────────────────────────────────

func TestProvider_SourceURL(t *testing.T) {
	name := uniqueName(t)
	Register(&stubProvider{name: name})
	p, err := Get(name)
	require.NoError(t, err)

	sp := SourceParams{
		Instrument: "EURUSD",
		Time:       time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		Timeframe:  "H1",
	}
	url := p.SourceURL(sp)
	assert.Contains(t, url, "EURUSD")
}
