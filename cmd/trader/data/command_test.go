package data_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/cmd/trader/internal/rootcmd"
	"github.com/rustyeddy/trader/marketdata"
)

// discard is a minimal io.Writer that keeps test output out of `go
// test -v`'s own output without importing io/ioutil or os for a
// throwaway /dev/null handle. Shared by every data_test file in this
// package (mutation_test.go, vertical_slice_test.go, log_test.go).
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

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

// runData executes rootcmd.New()'s tree with args prefixed by
// "data" and the given --store-root/--raw-root, returning stdout and
// any error. It is this file's own end-to-end harness: every test
// here drives the real command tree exactly the way a user would,
// rather than calling package functions directly (dataargs_test.go
// already covers those in isolation).
func runData(t *testing.T, storeRoot, rawRoot string, args ...string) (string, error) {
	t.Helper()
	root, cleanup := rootcmd.New()
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

func TestDataBars_JSONFormatProducesValidJSON(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"build", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08")
	require.NoError(t, err)

	out, err := runData(t, storeRoot, rawRoot,
		"bars", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08", "--format", "json")
	require.NoError(t, err)

	var decoded struct {
		Bars []struct {
			Open string `json:"open"`
		} `json:"bars"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	require.Len(t, decoded.Bars, 2, "the fixture's two H1 records for 2024-01-07")
	require.Equal(t, "1.1", decoded.Bars[0].Open)
}

func TestDataPlan_JSONFormatProducesValidJSON(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	out, err := runData(t, storeRoot, rawRoot,
		"plan", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08", "--format", "json")
	require.NoError(t, err)

	var decoded struct {
		Actions []struct {
			Kind string `json:"kind"`
		} `json:"actions"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	require.Len(t, decoded.Actions, 1)
	require.Equal(t, "normalize-canonical", decoded.Actions[0].Kind)
}

func TestDataBars_RejectsUnsupportedFormat(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	_, err := runData(t, storeRoot, rawRoot,
		"bars", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08", "--format", "xml")
	require.Error(t, err)
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

// TestDataBars_DefaultsStoreRootWhenOmitted is issue #141's own
// regression: --store-root (and --raw-root) used to be required, and
// omitting it failed with a config error before ever reaching Manager.
// t.Setenv("XDG_DATA_HOME", ...) isolates this test from a developer's
// real home directory the same way it would isolate it from an
// inherited TRADER_STORE_ROOT (t.Setenv always overrides for the
// duration of the test, regardless of the ambient environment, so no
// os.LookupEnv/Unsetenv dance is needed here). A missing dataset
// (ErrDataUnavailable) is the expected outcome against a fresh,
// auto-created, empty default data directory -- not a config error.
func TestDataBars_DefaultsStoreRootWhenOmitted(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	root, cleanup := rootcmd.New()
	defer func() { _ = cleanup() }()
	root.SetArgs([]string{"data", "bars", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	require.ErrorIs(t, err, marketdata.ErrDataUnavailable,
		"a default store root must be computed and used, reaching Manager rather than failing at config load")
}

func TestDataPlan_PropagatesCancelledContext(t *testing.T) {
	rawRoot := copyFixtureRaw(t)
	storeRoot := t.TempDir()

	root, cleanup := rootcmd.New()
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

// TestDataBars_NoWritesToDefaultStoreOrRawRootsOnReadCommands is the
// #142 review's own regression: TestDataBars_NoWritesToStoreOrRawOnReadCommands
// above only exercises the no-hidden-writes invariant with
// --store-root/--raw-root explicitly supplied. It cannot catch a
// defaulting path (issue #141) that creates either directory itself --
// exactly the real regression review caught in an earlier version of
// applyDefaultDataRoots. This test runs the same three read commands
// with neither flag set at all, against an isolated XDG_DATA_HOME, and
// asserts that neither computed default directory -- nor the data
// directory's own parent -- ever gets created.
func TestDataBars_NoWritesToDefaultStoreOrRawRootsOnReadCommands(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	for _, cmdArgs := range [][]string{
		{"bars", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08"},
		{"coverage", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08"},
		{"plan", "EURUSD", "H1", "--from", "2024-01-07", "--to", "2024-01-08"},
	} {
		root, cleanup := rootcmd.New()
		root.SetArgs(append([]string{"data"}, cmdArgs...))
		root.SetOut(new(discard))
		root.SetErr(new(discard))
		_ = root.ExecuteContext(context.Background())
		_ = cleanup()
	}

	_, err := os.Stat(filepath.Join(dataHome, "trader"))
	require.True(t, os.IsNotExist(err),
		"a read-only command must never create the default trader data directory")
}
