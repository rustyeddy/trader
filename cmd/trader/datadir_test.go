package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultTraderDataDir_PrefersXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg-home")

	dir, err := defaultTraderDataDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/xdg-home", "trader"), dir)
}

func TestDefaultTraderDataDir_FallsBackToHomeLocalShare(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home-dir")

	dir, err := defaultTraderDataDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/home-dir", ".local", "share", "trader"), dir)
}

// TestDefaultTraderDataDir_ReturnsErrorWhenHomeUnknown proves this
// reports a real error rather than panicking or silently returning an
// unusable path when neither XDG_DATA_HOME nor HOME can be resolved --
// os.UserHomeDir returns an explicit error for an empty $HOME on Unix,
// which this function must propagate, not ignore.
func TestDefaultTraderDataDir_ReturnsErrorWhenHomeUnknown(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")

	_, err := defaultTraderDataDir()
	require.Error(t, err)
}

func TestApplyDefaultDataRoots_FillsBothWhenEmpty(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)

	cfg := datasetConfig{Provider: "oanda"}
	require.NoError(t, applyDefaultDataRoots(&cfg))

	wantStoreRoot := filepath.Join(dataDir, "trader", "data")
	wantRawRoot := filepath.Join(dataDir, "trader", "raw", "oanda")
	assert.Equal(t, wantStoreRoot, cfg.StoreRoot)
	assert.Equal(t, wantRawRoot, cfg.RawRoot)

	assert.DirExists(t, wantStoreRoot, "the default store root must be created, not merely computed")
	assert.DirExists(t, wantRawRoot, "the default raw root must be created, not merely computed")
}

// TestApplyDefaultDataRoots_RawRootIsScopedByProvider proves the
// default raw root is not a fixed "oanda"-shaped path: a
// non-default Provider gets its own raw archive location, since raw
// archives are provider-specific data.
func TestApplyDefaultDataRoots_RawRootIsScopedByProvider(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)

	cfg := datasetConfig{Provider: "some-other-provider"}
	require.NoError(t, applyDefaultDataRoots(&cfg))

	assert.Equal(t, filepath.Join(dataDir, "trader", "raw", "some-other-provider"), cfg.RawRoot)
}

// TestApplyDefaultDataRoots_NeverTouchesExplicitPaths proves an
// explicitly configured StoreRoot/RawRoot is left completely alone --
// including never being auto-created -- so a caller's own typo in an
// explicit path still surfaces the same filesystem error it always
// did, rather than this function silently creating the wrong
// directory and masking the mistake.
func TestApplyDefaultDataRoots_NeverTouchesExplicitPaths(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)

	explicitStoreRoot := filepath.Join(t.TempDir(), "does-not-exist-store")
	explicitRawRoot := filepath.Join(t.TempDir(), "does-not-exist-raw")
	cfg := datasetConfig{StoreRoot: explicitStoreRoot, RawRoot: explicitRawRoot, Provider: "oanda"}

	require.NoError(t, applyDefaultDataRoots(&cfg))

	assert.Equal(t, explicitStoreRoot, cfg.StoreRoot)
	assert.Equal(t, explicitRawRoot, cfg.RawRoot)
	assert.NoDirExists(t, explicitStoreRoot, "an explicit path must never be auto-created")
	assert.NoDirExists(t, explicitRawRoot, "an explicit path must never be auto-created")
}

func TestApplyDefaultDataRoots_FillsOnlyTheEmptyField(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)

	explicitStoreRoot := t.TempDir()
	cfg := datasetConfig{StoreRoot: explicitStoreRoot, Provider: "oanda"}

	require.NoError(t, applyDefaultDataRoots(&cfg))

	assert.Equal(t, explicitStoreRoot, cfg.StoreRoot, "an already-set StoreRoot must not be overwritten")
	assert.Equal(t, filepath.Join(dataDir, "trader", "raw", "oanda"), cfg.RawRoot)
}

func TestApplyDefaultDataRoots_PropagatesDataDirError(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")

	cfg := datasetConfig{Provider: "oanda"}
	err := applyDefaultDataRoots(&cfg)
	require.Error(t, err)
}
