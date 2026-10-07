package stooq

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// archiveRow builds one native archive row for SPY on date (YYYYMMDD)
// with the given close; open/high/low bracket it so the row is plausible.
func archiveRow(date, close string) string {
	return "SPY.US,D," + date + ",000000," + close + "," + close + "," + close + "," + close + ",1000,0"
}

func writeArchive(t *testing.T, rows ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spy.us.txt")
	content := archiveHeader + "\n" + strings.Join(rows, "\n")
	if len(rows) > 0 {
		content += "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func partHash(t *testing.T, root string, year int, month time.Month) [32]byte {
	t.Helper()
	b, err := os.ReadFile(partitionPath(root, "SPY", year, month))
	require.NoError(t, err)
	return sha256.Sum256(b)
}

func partDates(t *testing.T, root string, year int, month time.Month) []string {
	t.Helper()
	snap, err := ReadPartitionSnapshot(context.Background(), root, "SPY", year, month)
	require.NoError(t, err)
	var out []string
	for _, r := range snap.Records {
		out = append(out, r.Time.Format("2006-01-02")+"="+r.Close.String())
	}
	return out
}

func seed(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "raw")
	_, err := MergeArchive(context.Background(), writeArchive(t,
		archiveRow("20200130", "100"), archiveRow("20200131", "101"),
		archiveRow("20200203", "102"), archiveRow("20200204", "103")), root, "SPY")
	require.NoError(t, err)
	return root
}

func TestMergeArchive_FirstImportWritesEverything(t *testing.T) {
	root := filepath.Join(t.TempDir(), "raw")
	r, err := MergeArchive(context.Background(), writeArchive(t,
		archiveRow("20200131", "101"), archiveRow("20200203", "102")), root, "spy")
	require.NoError(t, err)
	assert.Equal(t, 2, r.RowsImported)
	assert.Equal(t, 2, r.RowsAdded)
	assert.Equal(t, 2, r.MonthsWritten)
	assert.False(t, r.FullReimport)
	assert.Equal(t, []string{"2020-02-03=102"}, partDates(t, root, 2020, time.February))
}

func TestMergeArchive_AddsOnlyRowsAfterLastRawDate(t *testing.T) {
	ctx := context.Background()
	root := seed(t)
	janBefore := partHash(t, root, 2020, time.January)

	r, err := MergeArchive(ctx, writeArchive(t,
		archiveRow("20200130", "100"), archiveRow("20200131", "101"),
		archiveRow("20200203", "102"), archiveRow("20200204", "103"),
		archiveRow("20200205", "104"), archiveRow("20200302", "105")), root, "SPY")
	require.NoError(t, err)
	assert.Equal(t, 6, r.RowsImported)
	assert.Equal(t, 2, r.RowsAdded)
	assert.Equal(t, 2, r.MonthsWritten, "last existing month rewritten plus the new month")
	assert.False(t, r.FullReimport)
	assert.Equal(t, time.Date(2020, time.March, 2, 0, 0, 0, 0, time.UTC), r.LastDate)

	assert.Equal(t, janBefore, partHash(t, root, 2020, time.January), "earlier partitions are untouched")
	assert.Equal(t, []string{"2020-02-03=102", "2020-02-04=103", "2020-02-05=104"}, partDates(t, root, 2020, time.February))
	assert.Equal(t, []string{"2020-03-02=105"}, partDates(t, root, 2020, time.March))
}

func TestMergeArchive_RerunIsNoOp(t *testing.T) {
	ctx := context.Background()
	root := seed(t)
	export := writeArchive(t, archiveRow("20200203", "102"), archiveRow("20200204", "103"))
	febBefore := partHash(t, root, 2020, time.February)

	r, err := MergeArchive(ctx, export, root, "SPY")
	require.NoError(t, err)
	assert.Zero(t, r.RowsAdded)
	assert.Zero(t, r.MonthsWritten)
	assert.False(t, r.FullReimport)
	assert.Equal(t, 2, r.RowsImported)
	assert.Equal(t, febBefore, partHash(t, root, 2020, time.February))
}

func TestMergeArchive_OlderOrShorterExportNeverDeletes(t *testing.T) {
	ctx := context.Background()
	root := seed(t)
	janBefore, febBefore := partHash(t, root, 2020, time.January), partHash(t, root, 2020, time.February)

	r, err := MergeArchive(ctx, writeArchive(t, archiveRow("20200130", "100")), root, "SPY")
	require.NoError(t, err)
	assert.Zero(t, r.RowsAdded)
	assert.Zero(t, r.MonthsWritten)
	assert.Equal(t, janBefore, partHash(t, root, 2020, time.January))
	assert.Equal(t, febBefore, partHash(t, root, 2020, time.February))
}

func TestMergeArchive_EmptyExportWithExistingRawIsNoOp(t *testing.T) {
	root := seed(t)
	febBefore := partHash(t, root, 2020, time.February)
	r, err := MergeArchive(context.Background(), writeArchive(t), root, "SPY")
	require.NoError(t, err)
	assert.Zero(t, r.RowsImported)
	assert.Zero(t, r.MonthsWritten)
	assert.Equal(t, febBefore, partHash(t, root, 2020, time.February))
}

func TestMergeArchive_EmptyExportWithoutRawWritesNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "raw")
	r, err := MergeArchive(context.Background(), writeArchive(t), root, "SPY")
	require.NoError(t, err)
	assert.Zero(t, r.MonthsWritten)
	_, statErr := os.Stat(filepath.Join(root, "SPY"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestMergeArchive_AdjustedHistoryTriggersFullReimport(t *testing.T) {
	ctx := context.Background()
	root := seed(t)
	// A split halved every historical close; the export covers all history.
	r, err := MergeArchive(ctx, writeArchive(t,
		archiveRow("20200130", "50"), archiveRow("20200131", "50.5"),
		archiveRow("20200203", "51"), archiveRow("20200204", "51.5"),
		archiveRow("20200205", "52")), root, "SPY")
	require.NoError(t, err)
	assert.True(t, r.FullReimport)
	assert.Equal(t, 5, r.RowsAdded)
	assert.Equal(t, 2, r.MonthsWritten)
	assert.Equal(t, []string{"2020-01-30=50", "2020-01-31=50.5"}, partDates(t, root, 2020, time.January))
	assert.Equal(t, []string{"2020-02-03=51", "2020-02-04=51.5", "2020-02-05=52"}, partDates(t, root, 2020, time.February))
}

func TestMergeArchive_RevisedLastCloseWithNoNewRowsTriggersFullReimport(t *testing.T) {
	root := seed(t)
	r, err := MergeArchive(context.Background(), writeArchive(t,
		archiveRow("20200130", "100"), archiveRow("20200131", "101"),
		archiveRow("20200203", "102"), archiveRow("20200204", "103.5")), root, "SPY")
	require.NoError(t, err)
	assert.True(t, r.FullReimport)
	assert.Equal(t, []string{"2020-02-03=102", "2020-02-04=103.5"}, partDates(t, root, 2020, time.February))
}

func TestMergeArchive_MismatchWithPartialExportFailsWithoutWriting(t *testing.T) {
	root := seed(t)
	janBefore, febBefore := partHash(t, root, 2020, time.January), partHash(t, root, 2020, time.February)
	// Export starts after the raw archive's first date and disagrees on the overlap.
	_, err := MergeArchive(context.Background(), writeArchive(t,
		archiveRow("20200203", "51"), archiveRow("20200204", "51.5"), archiveRow("20200205", "52")), root, "SPY")
	require.ErrorIs(t, err, ErrAdjustmentMismatch)
	assert.Equal(t, janBefore, partHash(t, root, 2020, time.January))
	assert.Equal(t, febBefore, partHash(t, root, 2020, time.February))
}

func TestMergeArchive_ExportMissingLastRawDate(t *testing.T) {
	ctx := context.Background()
	t.Run("covering all history falls back to full re-import", func(t *testing.T) {
		root := seed(t)
		r, err := MergeArchive(ctx, writeArchive(t,
			archiveRow("20200130", "100"), archiveRow("20200131", "101"),
			archiveRow("20200203", "102"), archiveRow("20200205", "104")), root, "SPY")
		require.NoError(t, err)
		assert.True(t, r.FullReimport)
	})
	t.Run("not covering all history fails", func(t *testing.T) {
		root := seed(t)
		_, err := MergeArchive(ctx, writeArchive(t, archiveRow("20200203", "102"), archiveRow("20200205", "104")), root, "SPY")
		require.ErrorIs(t, err, ErrAdjustmentMismatch)
	})
}

func TestMergeArchive_UnorderedExportPicksLatestRawRow(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "raw")
	// A raw month preserved in source (descending) order: the latest date is first.
	require.NoError(t, WritePartition(ctx, root, "SPY", 2020, time.February, []Record{
		mustRecord(t, "2020-02-04", "103"), mustRecord(t, "2020-02-03", "102")}, false))
	r, err := MergeArchive(ctx, writeArchive(t,
		archiveRow("20200205", "104"), archiveRow("20200204", "103"), archiveRow("20200203", "102")), root, "SPY")
	require.NoError(t, err)
	assert.Equal(t, 1, r.RowsAdded)
	assert.False(t, r.FullReimport)
}

func TestMergeArchive_MalformedExportLeavesRawUnchanged(t *testing.T) {
	root := seed(t)
	febBefore := partHash(t, root, 2020, time.February)
	_, err := MergeArchive(context.Background(), writeArchive(t,
		archiveRow("20200205", "104"), "SPY.US,D,notadate,000000,1,1,1,1,1,0"), root, "SPY")
	require.ErrorIs(t, err, ErrMalformedData)
	assert.Equal(t, febBefore, partHash(t, root, 2020, time.February))
}

func TestMergeArchive_EmptyExistingPartitionIsMalformed(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "raw")
	require.NoError(t, WritePartition(ctx, root, "SPY", 2020, time.February, nil, false))
	_, err := MergeArchive(ctx, writeArchive(t, archiveRow("20200205", "104")), root, "SPY")
	require.ErrorIs(t, err, ErrMalformedData)
}

func TestMergeArchive_RejectsBadInput(t *testing.T) {
	root := filepath.Join(t.TempDir(), "raw")
	export := writeArchive(t, archiveRow("20200205", "104"))
	_, err := MergeArchive(context.Background(), export, root, "  ")
	require.ErrorIs(t, err, ErrMalformedData)
	_, err = MergeArchive(context.Background(), filepath.Join(t.TempDir(), "missing.txt"), root, "SPY")
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = MergeArchive(ctx, export, root, "SPY")
	require.ErrorIs(t, err, context.Canceled)
}

func TestRawMonths_IgnoresUnrelatedEntriesAndSorts(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "raw")
	for _, m := range []struct {
		y int
		m time.Month
	}{{2021, time.March}, {2020, time.December}, {2020, time.February}} {
		require.NoError(t, WritePartition(ctx, root, "SPY", m.y, m.m, []Record{mustRecord(t, "2020-02-03", "1")}, false))
	}
	for _, junk := range []string{"SPY/notes", "SPY/2020/13", "SPY/2020/aa", "SPY/abcd/01", "SPY/2020/05"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, junk), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "SPY", "2020", "stray.txt"), nil, 0o644))
	got, err := rawMonths(root, "SPY")
	require.NoError(t, err)
	assert.Equal(t, []monthKey{{2020, time.February}, {2020, time.December}, {2021, time.March}}, got)

	none, err := rawMonths(root, "QQQ")
	require.NoError(t, err)
	assert.Empty(t, none)
}

func mustRecord(t *testing.T, date, close string) Record {
	t.Helper()
	d, err := time.Parse("2006-01-02", date)
	require.NoError(t, err)
	rec, err := parseArchiveRow("x", 1, archiveRow(d.Format("20060102"), close), "SPY")
	require.NoError(t, err)
	return rec
}
