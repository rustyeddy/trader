package marketdata

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
	before := len(Names())
	Register(nil)
	assert.Equal(t, before, len(Names()), "registering nil should not change provider count")
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
	Register(&stubProvider{name: name})
	Register(&stubProvider{name: name}) // re-register same name
	names := Names()
	count := 0
	for _, n := range names {
		if n == name {
			count++
		}
	}
	assert.Equal(t, 1, count, "re-registering same name should not duplicate the entry")
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

// ── Names ─────────────────────────────────────────────────────────────────────

func TestNames_IncludesRegisteredProvider(t *testing.T) {
	name := uniqueName(t)
	Register(&stubProvider{name: name})
	assert.Contains(t, Names(), name)
}

func TestNames_MultipleProviders(t *testing.T) {
	a := uniqueName(t) + "-A"
	b := uniqueName(t) + "-B"
	Register(&stubProvider{name: a})
	Register(&stubProvider{name: b})
	names := Names()
	assert.Contains(t, names, a)
	assert.Contains(t, names, b)
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
