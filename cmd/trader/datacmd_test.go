package main

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// copyFixtureRaw copies this package's committed raw archive
// (testdata/raw/oanda, the same fixture service/marketdata's own tests
// use) into a fresh, writable temp directory, so every test starts
// from a clean, isolated slate and never mutates the checked-in
// fixture.
func copyFixtureRaw(t *testing.T) string {
	t.Helper()
	const src = "testdata/raw/oanda"
	dst := filepath.Join(t.TempDir(), "oanda")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	require.NoError(t, err)
	return dst
}

// runData executes newRootCmd()'s tree with args prefixed by
// "data" and the given --store-root/--raw-root, returning stdout and
// any error. It is this file's own end-to-end harness: every test
// here drives the real command tree exactly the way a user would,
// rather than calling package functions directly (dataargs_test.go
// already covers those in isolation).
func runData(t *testing.T, storeRoot, rawRoot string, args ...string) (string, error) {
	t.Helper()
	root, cleanup := newRootCmd()
	defer func() { _ = cleanup() }()

	full := append([]string{"data"}, args...)
	full = append(full, "--store-root", storeRoot)
	if rawRoot != "" {
		full = append(full, "--raw-root", rawRoot)
	}
	root.SetArgs(full)

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestDataBars_MissingCanonicalDataReturnsError(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"bars", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.Error(t, err, "canonical H1 was never built from the raw fixture")
}

func TestDataPlan_ReportsNormalizeRequiredFromExistingRaw(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	out, err := runData(t, storeRoot, rawRoot,
		"plan", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.NoError(t, err)
	require.Contains(t, out, "normalize-canonical")
}

func TestDataCoverage_ReportsMissingCanonicalPartition(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	out, err := runData(t, storeRoot, rawRoot,
		"coverage", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.NoError(t, err)
	require.Contains(t, out, "missing")
}

func TestDataBars_InvalidIntervalIsRejectedBeforeAnyServiceCall(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"bars", "EURUSD", "H99", "--from", "2024-01-07", "--to", "2024-01-08")
	require.Error(t, err)
}

func TestDataBars_InvalidInstrumentIsRejected(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"bars", "NOTREAL1", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.Error(t, err)
}

func TestDataBars_RequiresStoreRoot(t *testing.T) {
	// Guard against a developer's shell or CI environment already
	// having TRADER_STORE_ROOT set: config.Load's required check is a
	// presence check, not a zero-value check, so an inherited env var
	// would silently satisfy it and this test would pass for the wrong
	// reason (or fail outright) depending on the ambient environment.
	if v, ok := os.LookupEnv("TRADER_STORE_ROOT"); ok {
		require.NoError(t, os.Unsetenv("TRADER_STORE_ROOT"))
		t.Cleanup(func() { _ = os.Setenv("TRADER_STORE_ROOT", v) })
	}

	root, cleanup := newRootCmd()
	defer func() { _ = cleanup() }()
	root.SetArgs([]string{"data", "bars", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	require.Error(t, err, "--store-root is required and was never supplied")
}

func TestDataPlan_PropagatesCancelledContext(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	root, cleanup := newRootCmd()
	defer func() { _ = cleanup() }()
	root.SetArgs([]string{"data", "plan", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08",
		"--store-root", storeRoot, "--raw-root", rawRoot})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := root.ExecuteContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestDataBars_NoWritesToStoreOrRawOnReadCommands(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	for _, cmdArgs := range [][]string{
		{"bars", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08"},
		{"coverage", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08"},
		{"plan", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08"},
	} {
		_, _ = runData(t, storeRoot, rawRoot, cmdArgs...)
	}

	entries, err := os.ReadDir(storeRoot)
	require.NoError(t, err)
	require.Empty(t, entries, "read commands must never publish anything to the canonical store")
}
