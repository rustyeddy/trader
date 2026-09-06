package stooq

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspect_FindsWrittenPartitions(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	_, err := Import(ctx, "testdata/spy_us_d_sample.csv", root, "SPY")
	require.NoError(t, err)

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

	assert.Equal(t, time.June, inv.Partitions[1].Month)
	assert.Equal(t, 1, inv.Partitions[1].RowCount)
}

// TestInspect_MissingRootPropagatesError confirms Inspect itself does
// not special-case a missing root — the same as oanda.Inspect.
// Treating "nothing has been imported yet" as an empty archive is the
// marketdata-side dispatcher's responsibility (mirroring
// rawInventoryLookup's own os.Stat check ahead of oanda.Inspect), not
// this package's.
func TestInspect_MissingRootPropagatesError(t *testing.T) {
	_, err := Inspect(context.Background(), t.TempDir()+"/does-not-exist")
	assert.Error(t, err)
}

func TestInspect_ReportsMalformedPartitionWithoutAbortingWalk(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	_, err := Import(ctx, "testdata/spy_us_d_sample.csv", root, "SPY")
	require.NoError(t, err)

	// Corrupt the May partition directly on disk.
	path := partitionPath(root, "SPY", 2020, time.May)
	require.NoError(t, writeFile(path, "# schema=raw-v1 source=stooq instrument=SPY tf=d1 year=2020 month=05\n"+rawV1Header+"\nnot,a,valid,row,at,all\n"))

	inv, err := Inspect(ctx, root)
	require.NoError(t, err)
	require.Len(t, inv.Partitions, 2)

	var may, june Partition
	for _, p := range inv.Partitions {
		if p.Month == time.May {
			may = p
		} else {
			june = p
		}
	}
	assert.Equal(t, PartitionStatusMalformed, may.Status)
	assert.Error(t, may.Err)
	assert.Equal(t, PartitionStatusOK, june.Status)
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
