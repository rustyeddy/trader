package stooq

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePartitionFile(t *testing.T, root, symbol string, year int, month time.Month, body string) string {
	t.Helper()
	path := partitionPath(root, symbol, year, month)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestOpen_ReadsRecordsAndMeta(t *testing.T) {
	root := t.TempDir()
	path := writePartitionFile(t, root, "SPY", 2020, time.May,
		"# schema=raw-v1 source=stooq instrument=SPY tf=d1 year=2020 month=05\n"+
			rawV1Header+"\n2020-05-01,282.80,283.19,278.85,282.79,74424000\n")

	r, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	symbol, year, month := r.Meta()
	assert.Equal(t, "SPY", symbol)
	assert.Equal(t, 2020, year)
	assert.Equal(t, time.May, month)

	rec, err := r.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "282.8", rec.Open.String())

	_, err = r.Next(context.Background())
	assert.ErrorIs(t, err, io.EOF)

	// Close is idempotent.
	require.NoError(t, r.Close())
	require.NoError(t, r.Close())
}

func TestOpen_MissingFile(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "SPY", "2020", "05", "SPY-2020-05-d1.csv"))
	assert.Error(t, err)
}

func TestReader_NextAfterCloseErrors(t *testing.T) {
	root := t.TempDir()
	path := writePartitionFile(t, root, "SPY", 2020, time.May, rawV1Header+"\n2020-05-01,1,1,1,1,1\n")

	r, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	_, err = r.Next(context.Background())
	assert.Error(t, err)
}

func TestReader_SchemaCrossCheckMismatches(t *testing.T) {
	root := t.TempDir()

	cases := map[string]string{
		"wrong schema":     "# schema=raw-v2 source=stooq instrument=SPY tf=d1 year=2020 month=05\n",
		"wrong source":     "# schema=raw-v1 source=oanda instrument=SPY tf=d1 year=2020 month=05\n",
		"wrong instrument": "# schema=raw-v1 source=stooq instrument=QQQ tf=d1 year=2020 month=05\n",
		"wrong tf":         "# schema=raw-v1 source=stooq instrument=SPY tf=h1 year=2020 month=05\n",
		"wrong year":       "# schema=raw-v1 source=stooq instrument=SPY tf=d1 year=2021 month=05\n",
		"wrong month":      "# schema=raw-v1 source=stooq instrument=SPY tf=d1 year=2020 month=06\n",
	}
	for name, comment := range cases {
		t.Run(name, func(t *testing.T) {
			path := writePartitionFile(t, root, "SPY", 2020, time.May, comment+rawV1Header+"\n")
			_, err := Open(path)
			assert.ErrorIs(t, err, ErrMalformedData, name)
		})
	}
}

func TestReader_RejectsMissingColumnHeader(t *testing.T) {
	root := t.TempDir()
	path := writePartitionFile(t, root, "SPY", 2020, time.May, "\n")
	_, err := Open(path)
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestReader_RejectsWrongColumnHeader(t *testing.T) {
	root := t.TempDir()
	path := writePartitionFile(t, root, "SPY", 2020, time.May, "date,open,high,low,close\n")
	_, err := Open(path)
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestPartitionStatus_String(t *testing.T) {
	assert.Equal(t, "unknown", PartitionStatusUnknown.String())
	assert.Equal(t, "ok", PartitionStatusOK.String())
	assert.Equal(t, "unreadable", PartitionStatusUnreadable.String())
	assert.Equal(t, "malformed", PartitionStatusMalformed.String())
	assert.Equal(t, "PartitionStatus(9)", PartitionStatus(9).String())
}

func TestWritePartition_MustNotExistRejectsExisting(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	rec := Record{Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)}

	require.NoError(t, WritePartition(ctx, root, "SPY", 2020, time.May, []Record{rec}, true))
	err := WritePartition(ctx, root, "SPY", 2020, time.May, []Record{rec}, true)
	assert.ErrorIs(t, err, ErrPartitionAlreadyExists)
}

func TestWritePartition_ReplacesExistingWhenNotMustNotExist(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	first := Record{Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), Open: num.MustParsePrice("100")}
	second := Record{Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), Open: num.MustParsePrice("200")}

	require.NoError(t, WritePartition(ctx, root, "SPY", 2020, time.May, []Record{first}, true))
	require.NoError(t, WritePartition(ctx, root, "SPY", 2020, time.May, []Record{second}, false))

	snap, err := ReadPartitionSnapshot(ctx, root, "SPY", 2020, time.May)
	require.NoError(t, err)
	require.Len(t, snap.Records, 1)
	assert.Equal(t, "200", snap.Records[0].Open.String())
}

func TestReadPartitionSnapshot_MissingFile(t *testing.T) {
	_, err := ReadPartitionSnapshot(context.Background(), t.TempDir(), "SPY", 2020, time.May)
	assert.True(t, errors.Is(err, os.ErrNotExist))
}
