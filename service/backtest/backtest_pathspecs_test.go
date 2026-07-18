package backtestsvc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveBacktestConfigPaths_FileDirectoryAndGlobParity(t *testing.T) {
	dir := t.TempDir()

	fileA := filepath.Join(dir, "a.yml")
	fileB := filepath.Join(dir, "b.yaml")
	fileC := filepath.Join(dir, "c.json")
	other := filepath.Join(dir, "notes.txt")

	for _, path := range []string{fileA, fileB, fileC, other} {
		require.NoError(t, os.WriteFile(path, []byte("version: 1\nruns: []\n"), 0o644))
	}

	gotFile, err := ResolveBacktestConfigPaths([]string{fileA})
	require.NoError(t, err)
	assert.Equal(t, []string{fileA}, gotFile)

	gotDir, err := ResolveBacktestConfigPaths([]string{dir})
	require.NoError(t, err)
	assert.Equal(t, []string{fileA, fileB, fileC}, gotDir)

	gotGlob, err := ResolveBacktestConfigPaths([]string{filepath.Join(dir, "*.y*ml")})
	require.NoError(t, err)
	assert.Equal(t, []string{fileA, fileB}, gotGlob)
}

func TestResolveBacktestConfigPaths_DedupesAcrossSpecs(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.yml")
	fileB := filepath.Join(dir, "b.json")
	for _, path := range []string{fileA, fileB} {
		require.NoError(t, os.WriteFile(path, []byte("version: 1\nruns: []\n"), 0o644))
	}

	got, err := ResolveBacktestConfigPaths([]string{dir, fileA, filepath.Join(dir, "*.json")})
	require.NoError(t, err)
	assert.Equal(t, []string{fileA, fileB}, got)
}

func TestResolveBacktestConfigPaths_NoGlobMatchesReturnsError(t *testing.T) {
	_, err := ResolveBacktestConfigPaths([]string{filepath.Join(t.TempDir(), "*.yml")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "matched no config files")
}

func TestResolveBacktestConfigPaths_EmptyDirectoryReturnsError(t *testing.T) {
	_, err := ResolveBacktestConfigPaths([]string{t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contains no .yml/.yaml/.json files")
}

func TestResolveBacktestConfigPaths_BlankSpecReturnsError(t *testing.T) {
	_, err := ResolveBacktestConfigPaths([]string{"   "})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be blank")
}
