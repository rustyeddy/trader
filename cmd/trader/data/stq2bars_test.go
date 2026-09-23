package data

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindStooqArchiveFindsUniqueSymbolArchive(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "daily", "spy_us_d.zip")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("zip"), 0o644))

	got, err := findStooqArchive(root, "SPY")
	require.NoError(t, err)
	require.Equal(t, path, got)
}

func TestFindStooqArchiveRejectsAmbiguousMatches(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"spy_2024.zip", "spy_2025.zip"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("zip"), 0o644))
	}

	_, err := findStooqArchive(root, "SPY")
	require.Error(t, err)
	require.Contains(t, err.Error(), "multiple Stooq archives")
}

func TestFindStooqArchiveRequiresConfiguredRoot(t *testing.T) {
	_, err := findStooqArchive("", "SPY")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--archive")
}
