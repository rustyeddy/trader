package trader

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDukasfileInstrument_Ungated(t *testing.T) {
	t.Parallel()

	df := newDatafile("GBPUSD", time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC))
	assert.Equal(t, "GBPUSD", df.Instrument())
}

func TestDukasfileIsValid_EmptyFile_MarketClosed(t *testing.T) {

	s := useTempStore(t)
	ts := time.Date(2026, 1, 4, 12, 0, 0, 0, time.UTC) // Sunday
	df := newDatafile("EURUSD", ts)
	path := s.PathForAsset(df.Key())
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	err := df.IsValid(context.Background())
	require.NoError(t, err)
}

func TestDukasfileIsValid_EmptyFile_MarketOpen(t *testing.T) {

	s := useTempStore(t)
	ts := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC) // Wednesday, not near holiday closure
	df := newDatafile("EURUSD", ts)
	path := s.PathForAsset(df.Key())
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	err := df.IsValid(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty file outside market-closed hours")
}
