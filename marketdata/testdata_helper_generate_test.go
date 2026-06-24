package marketdata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSyntheticYearTestData_Success(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	paths, err := GenerateSyntheticYearTestData(base, "EURUSD", 2026, market.D1)
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	for _, p := range paths {
		_, statErr := os.Stat(p)
		require.NoError(t, statErr)
	}
}

func TestGenerateSyntheticYearTestData_MkdirAllError(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	filePath := filepath.Join(base, "not-a-dir")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))

	_, err := GenerateSyntheticYearTestData(filepath.Join(filePath, "child"), "EURUSD", 2026, market.H1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create testdata dir")
}
