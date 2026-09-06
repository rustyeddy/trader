package stooq

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport_SmallFixtureSplitsIntoMonthlyPartitions(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	result, err := Import(ctx, "testdata/spy_us_d_sample.csv", rawRoot, "spy")
	require.NoError(t, err)

	assert.Equal(t, 3, result.RowsImported)
	assert.Equal(t, 2, result.MonthsWritten) // 2020-05 and 2020-06
	assert.True(t, result.FirstDate.Equal(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)))
	assert.True(t, result.LastDate.Equal(time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)))

	snap, err := ReadPartitionSnapshot(ctx, rawRoot, "SPY", 2020, time.May)
	require.NoError(t, err)
	require.Len(t, snap.Records, 2)
	assert.True(t, snap.Records[0].Time.Equal(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, "282.8", snap.Records[0].Open.String())
	assert.Equal(t, "283.19", snap.Records[0].High.String())
	assert.Equal(t, "278.85", snap.Records[0].Low.String())
	assert.Equal(t, "282.79", snap.Records[0].Close.String())
	assert.Equal(t, int64(74424000), snap.Records[0].Volume)
	assert.NotEmpty(t, snap.Fingerprint)

	snapJune, err := ReadPartitionSnapshot(ctx, rawRoot, "SPY", 2020, time.June)
	require.NoError(t, err)
	require.Len(t, snapJune.Records, 1)
}

func TestImport_UppercasesSymbol(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	_, err := Import(ctx, "testdata/spy_us_d_sample.csv", rawRoot, "spy")
	require.NoError(t, err)

	// A lowercase symbol import must be readable under the canonical
	// uppercase partition path.
	_, err = ReadPartitionSnapshot(ctx, rawRoot, "SPY", 2020, time.May)
	require.NoError(t, err)
}

func TestImport_RejectsWrongHeader(t *testing.T) {
	dir := t.TempDir()
	path := writeTempCSV(t, dir, "bad.csv", "date,open,high,low,close,volume\n2020-05-01,1,2,0,1,100\n")

	_, err := Import(context.Background(), path, t.TempDir(), "SPY")
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestImport_RejectsMalformedRow(t *testing.T) {
	dir := t.TempDir()
	path := writeTempCSV(t, dir, "bad.csv", nativeHeader+"\n2020-05-01,not-a-number,283.19,278.85,282.79,74424000\n")

	_, err := Import(context.Background(), path, t.TempDir(), "SPY")
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestImport_RejectsWrongFieldCount(t *testing.T) {
	dir := t.TempDir()
	path := writeTempCSV(t, dir, "bad.csv", nativeHeader+"\n2020-05-01,282.80,283.19,278.85,282.79\n")

	_, err := Import(context.Background(), path, t.TempDir(), "SPY")
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestImport_RejectsNegativeVolume(t *testing.T) {
	dir := t.TempDir()
	path := writeTempCSV(t, dir, "bad.csv", nativeHeader+"\n2020-05-01,282.80,283.19,278.85,282.79,-1\n")

	_, err := Import(context.Background(), path, t.TempDir(), "SPY")
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestImport_RejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempCSV(t, dir, "empty.csv", "")

	_, err := Import(context.Background(), path, t.TempDir(), "SPY")
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestImport_RejectsMissingSymbol(t *testing.T) {
	_, err := Import(context.Background(), "testdata/spy_us_d_sample.csv", t.TempDir(), "  ")
	assert.ErrorIs(t, err, ErrMalformedData)
}

// TestImport_RerunOverwritesPreviousPartitions confirms Import's own
// documented "always overwrite, no incremental mode" contract: a
// second Import call with different data for the same month replaces
// the first, rather than merging with or erroring on the existing
// partition.
func TestImport_RerunOverwritesPreviousPartitions(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()
	dir := t.TempDir()

	first := writeTempCSV(t, dir, "first.csv", nativeHeader+"\n2020-05-01,100,110,90,105,1000\n")
	_, err := Import(ctx, first, rawRoot, "SPY")
	require.NoError(t, err)

	second := writeTempCSV(t, dir, "second.csv", nativeHeader+"\n2020-05-01,200,210,190,205,2000\n")
	_, err = Import(ctx, second, rawRoot, "SPY")
	require.NoError(t, err)

	snap, err := ReadPartitionSnapshot(ctx, rawRoot, "SPY", 2020, time.May)
	require.NoError(t, err)
	require.Len(t, snap.Records, 1)
	assert.Equal(t, "200", snap.Records[0].Open.String())
}

func writeTempCSV(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}
