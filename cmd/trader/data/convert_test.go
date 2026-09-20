package data

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractStooqMember_SelectsSPYAndPreservesContent(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "daily.zip")
	f, err := os.Create(archivePath)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, err := zw.Create("data/daily/us/nyse etfs/2/spy.us.txt")
	require.NoError(t, err)
	_, err = w.Write([]byte("<TICKER>,<PER>\nSPY.US,D\n"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	destination := t.TempDir()
	path, err := extractStooqMember(context.Background(), archivePath, "SPY", destination)
	require.NoError(t, err)
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "<TICKER>,<PER>\nSPY.US,D\n", string(contents))
}

func TestExtractStooqMemberReportsMissingSymbol(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "daily.zip")
	f, err := os.Create(archivePath)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	_, err = zw.Create("data/daily/us/nyse etfs/2/qqq.us.txt")
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	_, err = extractStooqMember(context.Background(), archivePath, "SPY", t.TempDir())
	require.ErrorContains(t, err, "contains no spy.us.txt member")
}
