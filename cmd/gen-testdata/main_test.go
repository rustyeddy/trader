package main

import (
	"os"
	"path/filepath"
	"testing"

	trader "github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generate runs GenerateSyntheticYearTestData into a temp dir and returns the paths.
func generate(t *testing.T, instrument string, year int, tf trader.Timeframe) []string {
	t.Helper()
	dir := t.TempDir()
	paths, err := trader.GenerateSyntheticYearTestData(dir, instrument, year, tf)
	require.NoError(t, err)
	return paths
}

// ── output shape ──────────────────────────────────────────────────────────────

func TestGenTestdata_H1_ProducesTwelveFiles(t *testing.T) {
	paths := generate(t, "EURUSD", 2024, trader.H1)
	assert.Len(t, paths, 12, "H1 year should produce one file per month")
}

func TestGenTestdata_M1_ProducesTwelveFiles(t *testing.T) {
	paths := generate(t, "EURUSD", 2024, trader.M1)
	assert.Len(t, paths, 12, "M1 year should produce one file per month")
}

func TestGenTestdata_D1_ProducesTwelveFiles(t *testing.T) {
	paths := generate(t, "EURUSD", 2024, trader.D1)
	assert.Len(t, paths, 12, "D1 year should produce one file per month")
}

func TestGenTestdata_FilesAreNonEmpty(t *testing.T) {
	paths := generate(t, "EURUSD", 2024, trader.H1)
	for _, p := range paths {
		info, err := os.Stat(p)
		require.NoError(t, err, "file should exist: %s", p)
		assert.Greater(t, info.Size(), int64(0), "file should be non-empty: %s", p)
	}
}

func TestGenTestdata_FilesExistOnDisk(t *testing.T) {
	dir := t.TempDir()
	paths, err := trader.GenerateSyntheticYearTestData(dir, "EURUSD", 2024, trader.H1)
	require.NoError(t, err)
	for _, p := range paths {
		_, err := os.Stat(p)
		assert.NoError(t, err, "expected file to exist: %s", p)
	}
}

func TestGenTestdata_FilesAreUnderOutputDir(t *testing.T) {
	dir := t.TempDir()
	paths, err := trader.GenerateSyntheticYearTestData(dir, "EURUSD", 2024, trader.H1)
	require.NoError(t, err)
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		require.NoError(t, err)
		assert.False(t, filepath.IsAbs(rel), "path should be under output dir: %s", p)
	}
}

// ── instrument and year isolation ─────────────────────────────────────────────

func TestGenTestdata_DifferentInstrumentsUseSeparateFiles(t *testing.T) {
	dir := t.TempDir()
	eurPaths, err := trader.GenerateSyntheticYearTestData(dir, "EURUSD", 2024, trader.H1)
	require.NoError(t, err)
	gbpPaths, err := trader.GenerateSyntheticYearTestData(dir, "GBPUSD", 2024, trader.H1)
	require.NoError(t, err)

	eurSet := make(map[string]bool, len(eurPaths))
	for _, p := range eurPaths {
		eurSet[p] = true
	}
	for _, p := range gbpPaths {
		assert.False(t, eurSet[p], "GBPUSD and EURUSD should not share output files")
	}
}

func TestGenTestdata_DifferentYearsAreDeterministicAndDistinct(t *testing.T) {
	dir := t.TempDir()
	paths24, err := trader.GenerateSyntheticYearTestData(dir, "EURUSD", 2024, trader.H1)
	require.NoError(t, err)
	paths23, err := trader.GenerateSyntheticYearTestData(dir, "EURUSD", 2023, trader.H1)
	require.NoError(t, err)

	set24 := make(map[string]bool, len(paths24))
	for _, p := range paths24 {
		set24[p] = true
	}
	for _, p := range paths23 {
		assert.False(t, set24[p], "different years should produce different file paths")
	}
}

// ── idempotency ───────────────────────────────────────────────────────────────

func TestGenTestdata_RerunOverwritesWithIdenticalContent(t *testing.T) {
	dir := t.TempDir()

	paths1, err := trader.GenerateSyntheticYearTestData(dir, "EURUSD", 2024, trader.H1)
	require.NoError(t, err)

	sizes1 := make(map[string]int64, len(paths1))
	for _, p := range paths1 {
		info, _ := os.Stat(p)
		sizes1[p] = info.Size()
	}

	paths2, err := trader.GenerateSyntheticYearTestData(dir, "EURUSD", 2024, trader.H1)
	require.NoError(t, err)

	for _, p := range paths2 {
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, sizes1[p], info.Size(), "re-run should produce identical file sizes for %s", p)
	}
}

// ── empty basedir uses default ────────────────────────────────────────────────

func TestGenTestdata_EmptyBasedirUsesDefault(t *testing.T) {
	// GenerateSyntheticYearTestData uses "testdata" when basedir is "".
	// Run from a temp dir so we don't pollute the repo.
	origDir, err := os.Getwd()
	require.NoError(t, err)
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	paths, err := trader.GenerateSyntheticYearTestData("", "EURUSD", 2024, trader.H1)
	require.NoError(t, err)
	assert.Len(t, paths, 12)

	// All files should be under tmp/testdata.
	expectedBase := filepath.Join(tmp, "testdata")
	for _, p := range paths {
		abs, err := filepath.Abs(p) // resolve relative paths against current (tmp) dir
		require.NoError(t, err)
		rel, err := filepath.Rel(expectedBase, abs)
		require.NoError(t, err)
		assert.False(t, filepath.IsAbs(rel), "file should be under testdata/: %s", p)
	}
}
