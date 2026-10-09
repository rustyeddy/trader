package alpaca

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspect_FindsWrittenPartitions(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	may := []Record{
		{Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), Open: num.MustParsePrice("282.80")},
		{Time: time.Date(2020, 5, 4, 0, 0, 0, 0, time.UTC), Open: num.MustParsePrice("283.00")},
	}
	june := []Record{
		{Time: time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC), Open: num.MustParsePrice("284.00")},
	}
	require.NoError(t, WritePartition(ctx, root, "SPY", 2020, time.May, FeedIEX, may, true))
	require.NoError(t, WritePartition(ctx, root, "SPY", 2020, time.June, FeedIEX, june, true))

	inv, err := Inspect(ctx, root)
	require.NoError(t, err)
	require.Len(t, inv.Partitions, 2)

	assert.Equal(t, "SPY", inv.Partitions[0].Symbol)
	assert.Equal(t, 2020, inv.Partitions[0].Year)
	assert.Equal(t, time.May, inv.Partitions[0].Month)
	assert.Equal(t, PartitionStatusOK, inv.Partitions[0].Status)
	assert.Equal(t, 2, inv.Partitions[0].RowCount)
	assert.True(t, inv.Partitions[0].LastComplete)
	assert.NotEmpty(t, inv.Partitions[0].Fingerprint)
	assert.Equal(t, FeedIEX, inv.Partitions[0].Feed)

	assert.Equal(t, time.June, inv.Partitions[1].Month)
	assert.Equal(t, 1, inv.Partitions[1].RowCount)
}

// TestInspect_MissingRootPropagatesError confirms Inspect itself does not
// special-case a missing root — the same as oanda/stooq's own Inspect.
func TestInspect_MissingRootPropagatesError(t *testing.T) {
	_, err := Inspect(context.Background(), t.TempDir()+"/does-not-exist")
	assert.Error(t, err)
}

func TestInspect_ReportsMalformedPartitionWithoutAbortingWalk(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	may := []Record{{Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), Open: num.MustParsePrice("282.80")}}
	june := []Record{{Time: time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC), Open: num.MustParsePrice("284.00")}}
	require.NoError(t, WritePartition(ctx, root, "SPY", 2020, time.May, FeedIEX, may, true))
	require.NoError(t, WritePartition(ctx, root, "SPY", 2020, time.June, FeedIEX, june, true))

	path := partitionPath(root, "SPY", 2020, time.May)
	require.NoError(t, writeFile(path, "# schema=raw-v1 source=alpaca instrument=SPY tf=d1 year=2020 month=05\n"+rawV1Header+"\nnot,a,valid,row,at,all\n"))

	inv, err := Inspect(ctx, root)
	require.NoError(t, err)
	require.Len(t, inv.Partitions, 2)

	var mayPart, junePart Partition
	for _, p := range inv.Partitions {
		if p.Month == time.May {
			mayPart = p
		} else {
			junePart = p
		}
	}
	assert.Equal(t, PartitionStatusMalformed, mayPart.Status)
	assert.Error(t, mayPart.Err)
	assert.Equal(t, PartitionStatusOK, junePart.Status)
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestInspectMatching_RejectedSymbolsAreNeverRead proves the filter runs
// before any file I/O: the read hook records every file read, and a
// rejected symbol's file never appears in it.
func TestInspectMatching_RejectedSymbolsAreNeverRead(t *testing.T) {
	root := t.TempDir()
	spy := writePartitionFile(t, root, "SPY", 2020, time.May, "not a valid partition\n")
	qqq := writePartitionFile(t, root, "QQQ", 2020, time.May, "not a valid partition\n")
	reads := recordReads(t)

	ctx := context.Background()
	got, err := InspectMatching(ctx, root, func(symbol string) bool { return symbol == "SPY" })
	require.NoError(t, err)
	require.Len(t, got.Partitions, 1)
	assert.Equal(t, "SPY", got.Partitions[0].Symbol)
	assert.Equal(t, []string{spy}, *reads, "only the accepted file is read")

	*reads = nil
	all, err := Inspect(ctx, root)
	require.NoError(t, err)
	require.Len(t, all.Partitions, 2)
	assert.ElementsMatch(t, []string{spy, qqq}, *reads, "without a filter every file is read")
}

func TestInspectMatching_NilMatchesEverything(t *testing.T) {
	root := t.TempDir()
	writePartitionFile(t, root, "SPY", 2020, time.May, "not a valid partition\n")
	a, err := Inspect(context.Background(), root)
	require.NoError(t, err)
	b, err := InspectMatching(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, a, b)
}

// recordReads replaces readFile for the test and returns the paths read.
func recordReads(t *testing.T) *[]string {
	t.Helper()
	var reads []string
	orig := readFile
	readFile = func(path string) ([]byte, error) {
		reads = append(reads, path)
		return os.ReadFile(path)
	}
	t.Cleanup(func() { readFile = orig })
	return &reads
}
