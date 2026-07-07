package dukascopy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useTempStore(t *testing.T) *marketdata.Store {
	t.Helper()
	s := marketdata.NewStoreAt(t.TempDir())
	restore := marketdata.SwapStore(s)
	t.Cleanup(restore)
	return s
}

func TestFileIsValid_EmptyFile_MarketClosed(t *testing.T) {
	s := useTempStore(t)
	ts := time.Date(2026, 1, 4, 12, 0, 0, 0, time.UTC) // Sunday
	f := NewFile("EURUSD", ts)
	path, err := s.PathForAsset(f.Key())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	err = f.IsValid(context.Background())
	require.NoError(t, err)
}

func TestFileIsValid_EmptyFile_MarketOpen(t *testing.T) {
	s := useTempStore(t)
	ts := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC) // Wednesday
	f := NewFile("EURUSD", ts)
	path, err := s.PathForAsset(f.Key())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	err = f.IsValid(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty file outside market-closed hours")
}
