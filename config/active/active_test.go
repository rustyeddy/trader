package active

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_MissingFileReturnsZeroValue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sel, err := Load()
	require.NoError(t, err)
	assert.True(t, sel.IsZero())
}

func TestSaveThenLoad_RoundTrips(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	want := Selection{Broker: "oanda", AccountID: "101-004-12345-001"}
	require.NoError(t, Save(want))

	got, err := Load()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSave_CreatesConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, Save(Selection{Broker: "oanda", AccountID: "acc-1"}))

	_, err := os.Stat(filepath.Join(home, ".config", "trader", "active.json"))
	require.NoError(t, err)
}

func TestSave_DoesNotUseYAMLExtension(t *testing.T) {
	// Guards against ever colliding with config.LoadGlobalConfig's
	// ~/.config/trader/*.yml merge glob.
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, Save(Selection{Broker: "oanda", AccountID: "acc-1"}))

	matches, err := filepath.Glob(filepath.Join(home, ".config", "trader", "*.yml"))
	require.NoError(t, err)
	assert.Empty(t, matches, "active state must not be written as a .yml file")
}

func TestSelection_IsZero(t *testing.T) {
	assert.True(t, Selection{}.IsZero())
	assert.False(t, Selection{Broker: "oanda"}.IsZero())
	assert.False(t, Selection{AccountID: "acc-1"}.IsZero())
}
