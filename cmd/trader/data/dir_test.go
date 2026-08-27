package data

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
}

// TestApplyDefaultDataRoots_NeverCreatesDirectories is the direct
// regression for the #142 review finding: an earlier version of this
// function called os.MkdirAll on a defaulted root, which meant
// read-only commands (bars, coverage, plan) created directories on a
// fresh install -- a real violation of the no-hidden-writes invariant
// M2.5 established for read commands. This function must compute a
// path only; the actual gap that MkdirAll was working around is fixed
// separately, at its real source (Manager's own rawInventoryLookup,
// marketdata/coverage.go).
func TestApplyDefaultDataRoots_NeverCreatesDirectories(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)

	cfg := datasetConfig{Provider: "oanda"}
	require.NoError(t, applyDefaultDataRoots(&cfg))

	assert.NoDirExists(t, cfg.StoreRoot, "applyDefaultDataRoots must never create a directory itself")
	assert.NoDirExists(t, cfg.RawRoot, "applyDefaultDataRoots must never create a directory itself")
}

// TestApplyDefaultDataRoots_ExplicitEmptyStringIsTreatedAsUnset
// documents and pins a deliberate decision, raised in #142's own
// review: config.Load cannot distinguish "the caller never supplied
// --store-root/TRADER_STORE_ROOT at all" from "the caller explicitly
// supplied an empty value" -- a plain string field carries no such
// presence information once decoded, and datasetConfig's StoreRoot/
// RawRoot fields carry no required:"true" tag to make an empty value
// a load-time error either. An empty string in either field is
// therefore always treated as "use the computed default," regardless
// of how it became empty. This is judged acceptable specifically
// because applyDefaultDataRoots no longer creates any directory
// (see TestApplyDefaultDataRoots_NeverCreatesDirectories): the
// original review concern was that auto-creation could mask a
// misconfiguration behind a silently-created directory, and that risk
// no longer exists now that nothing is created here.
func TestApplyDefaultDataRoots_ExplicitEmptyStringIsTreatedAsUnset(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)

	cfg := datasetConfig{StoreRoot: "", RawRoot: "", Provider: "oanda"}
	require.NoError(t, applyDefaultDataRoots(&cfg))

	assert.Equal(t, filepath.Join(dataDir, "trader", "data"), cfg.StoreRoot)
	assert.Equal(t, filepath.Join(dataDir, "trader", "raw", "oanda"), cfg.RawRoot)
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
// explicitly configured, non-empty StoreRoot/RawRoot is left
// completely alone -- neither overwritten with a computed default nor
// auto-created -- so a caller's own typo in an explicit path still
// surfaces the same filesystem error it always did.
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
