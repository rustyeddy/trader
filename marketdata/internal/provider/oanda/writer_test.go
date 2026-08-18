package oanda

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRecord(at time.Time, complete bool) Record {
	return Record{
		Time:     at,
		BidOpen:  num.MustParsePrice("1.10000"),
		BidHigh:  num.MustParsePrice("1.10100"),
		BidLow:   num.MustParsePrice("1.09900"),
		BidClose: num.MustParsePrice("1.10050"),
		AskOpen:  num.MustParsePrice("1.10010"),
		AskHigh:  num.MustParsePrice("1.10110"),
		AskLow:   num.MustParsePrice("1.09910"),
		AskClose: num.MustParsePrice("1.10060"),
		Volume:   100,
		Complete: complete,
	}
}

func TestWritePartition_NewFileRoundTrips(t *testing.T) {
	root := t.TempDir()
	records := []Record{
		testRecord(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), true),
		testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true), // unsorted on purpose
	}

	require.NoError(t, WritePartition(context.Background(), root, "EURUSD", RawH1, 2024, time.January, records, true))

	got, err := ReadPartitionRecords(context.Background(), root, "EURUSD", RawH1, 2024, time.January)
	require.NoError(t, err)
	require.Len(t, got, 2)
	// Sorted ascending regardless of input order.
	assert.True(t, got[0].Time.Before(got[1].Time))
	assert.True(t, got[0].Time.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, records[0].BidOpen.String(), got[0].BidOpen.String())
}

func TestWritePartition_MustNotExistRejectsExisting(t *testing.T) {
	root := t.TempDir()
	records := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	require.NoError(t, WritePartition(context.Background(), root, "EURUSD", RawH1, 2024, time.January, records, true))

	err := WritePartition(context.Background(), root, "EURUSD", RawH1, 2024, time.January, records, true)
	assert.ErrorIs(t, err, ErrPartitionAlreadyExists)
}

// TestWritePartition_ConcurrentMustNotExistIsRaceFree is the regression
// for a design review finding: an initial os.Stat check followed later
// by os.Rename leaves a window in which a file created by a concurrent
// writer would be silently replaced, defeating mustNotExist's whole
// purpose. With the Link-based commit, exactly one of N concurrent
// callers targeting the same path must succeed and every other must
// fail with ErrPartitionAlreadyExists — never a corrupted or
// unexpectedly-replaced file.
func TestWritePartition_ConcurrentMustNotExistIsRaceFree(t *testing.T) {
	root := t.TempDir()
	const n = 8

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			records := []Record{testRecord(time.Date(2024, 1, 1, i, 0, 0, 0, time.UTC), true)}
			errs[i] = WritePartition(context.Background(), root, "EURUSD", RawH1, 2024, time.January, records, true)
		}(i)
	}
	wg.Wait()

	successes, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrPartitionAlreadyExists):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	assert.Equal(t, 1, successes, "exactly one concurrent writer must win")
	assert.Equal(t, n-1, conflicts, "every other writer must see ErrPartitionAlreadyExists")

	got, err := ReadPartitionRecords(context.Background(), root, "EURUSD", RawH1, 2024, time.January)
	require.NoError(t, err)
	require.Len(t, got, 1, "the file must hold exactly one writer's content, not a mix")
}

func TestWritePartition_ExtendReplacesExisting(t *testing.T) {
	root := t.TempDir()
	first := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	require.NoError(t, WritePartition(context.Background(), root, "EURUSD", RawH1, 2024, time.January, first, true))

	merged := append(first, testRecord(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), true))
	require.NoError(t, WritePartition(context.Background(), root, "EURUSD", RawH1, 2024, time.January, merged, false))

	got, err := ReadPartitionRecords(context.Background(), root, "EURUSD", RawH1, 2024, time.January)
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestReadPartitionRecords_MissingFile(t *testing.T) {
	root := t.TempDir()
	_, err := ReadPartitionRecords(context.Background(), root, "EURUSD", RawH1, 2024, time.January)
	assert.True(t, errors.Is(err, fs.ErrNotExist), "want fs.ErrNotExist, got %v", err)
}

func TestWritePartition_EmptyRecordsProducesValidHeaderOnlyFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, WritePartition(context.Background(), root, "EURUSD", RawH1, 2024, time.January, nil, true))
	got, err := ReadPartitionRecords(context.Background(), root, "EURUSD", RawH1, 2024, time.January)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestWritePartition_PathMatchesArchiveLayout(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, WritePartition(context.Background(), root, "EURUSD", RawD1, 2024, time.March, []Record{
		testRecord(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), true),
	}, true))

	want := root + "/EURUSD/2024/03/EURUSD-2024-03-d1.csv"
	_, err := os.Stat(want)
	require.NoError(t, err, "expected file at %s", want)

	// And Inspect (the archive-wide inventory walker) must recognize it.
	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)
	p := findPartition(t, inv, "EURUSD", RawD1, 2024, time.March)
	assert.Equal(t, PartitionStatusOK, p.Status)
	assert.Equal(t, eurusdID(), p.Instrument)
}

// --- Atomicity ---

// nthCancelContext reports context.Canceled from Err() once its
// remaining count is exhausted, mirroring marketdata/store_csv_test.go's
// own helper (unexported per-package, so duplicated here rather than
// shared across the marketdata/oanda boundary).
type nthCancelContext struct {
	context.Context
	remaining int
}

func (c *nthCancelContext) Err() error {
	if c.remaining <= 0 {
		return context.Canceled
	}
	c.remaining--
	return nil
}

func TestWritePartition_CancelledBeforeWriteLeavesExistingFileIntact(t *testing.T) {
	root := t.TempDir()
	original := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	require.NoError(t, WritePartition(context.Background(), root, "EURUSD", RawH1, 2024, time.January, original, true))

	ctx := &nthCancelContext{Context: context.Background(), remaining: 0}
	err := WritePartition(ctx, root, "EURUSD", RawH1, 2024, time.January, append(original, testRecord(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), true)), false)
	assert.ErrorIs(t, err, context.Canceled)

	got, err := ReadPartitionRecords(context.Background(), root, "EURUSD", RawH1, 2024, time.January)
	require.NoError(t, err)
	require.Len(t, got, 1, "original file must be untouched")
}

func TestWritePartition_CancelledJustBeforeRenameLeavesExistingFileIntact(t *testing.T) {
	root := t.TempDir()
	original := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	require.NoError(t, WritePartition(context.Background(), root, "EURUSD", RawH1, 2024, time.January, original, true))

	// remaining=2: the initial ctx.Err() check, then one per-record
	// check during encode (one record) both succeed; cancellation lands
	// on the commit-point check immediately before the rename.
	ctx := &nthCancelContext{Context: context.Background(), remaining: 2}
	err := WritePartition(ctx, root, "EURUSD", RawH1, 2024, time.January, append(original, testRecord(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), true)), false)
	assert.ErrorIs(t, err, context.Canceled)

	got, err := ReadPartitionRecords(context.Background(), root, "EURUSD", RawH1, 2024, time.January)
	require.NoError(t, err)
	require.Len(t, got, 1, "original file must still be intact; cancelled write must not have replaced it")
}

func TestFingerprintPartition(t *testing.T) {
	root := t.TempDir()
	records := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	require.NoError(t, WritePartition(context.Background(), root, "EURUSD", RawH1, 2024, time.January, records, true))

	got, err := FingerprintPartition(root, "EURUSD", RawH1, 2024, time.January)
	require.NoError(t, err)
	assert.Regexp(t, "^sha256:[0-9a-f]{64}$", got)

	// Must match Inspect's own fingerprint for the identical file.
	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)
	p := findPartition(t, inv, "EURUSD", RawH1, 2024, time.January)
	assert.Equal(t, p.Fingerprint, got)
}

func TestFingerprintPartition_MissingFile(t *testing.T) {
	root := t.TempDir()
	_, err := FingerprintPartition(root, "EURUSD", RawH1, 2024, time.January)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}
