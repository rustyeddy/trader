package oanda

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A file the writer produces from a real archive file's records is
// byte-identical to that file, including the daily file's tf=d header.
func TestWritePartition_ReproducesArchiveFixturesByteForByte(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.file, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", f.file))
			require.NoError(t, err)
			root := storetest.Root(t)
			_, records, err := ReadFile(context.Background(), fixture(t, f.file))
			require.NoError(t, err)

			require.NoError(t, WritePartition(context.Background(), PartitionKey{f.symbol, f.interval, f.year, f.month}, records, true))

			path, err := PartitionKey{f.symbol, f.interval, f.year, f.month}.Path()
			require.NoError(t, err)
			path = filepath.Join(root, path)
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(want), string(got))
		})
	}
}

func TestRawTFSpellsTheDailyTokenAsInTheArchive(t *testing.T) {
	assert.Equal(t, "d", rawTF(RawD1))
	assert.Equal(t, "h1", rawTF(RawH1))
	assert.Equal(t, "m1", rawTF(RawM1))
	assert.Equal(t, "h4", rawTF(RawH4))
}

func TestWritePartition_RefusesAnOutOfScopeSymbolAndWritesNothing(t *testing.T) {
	root := storetest.Root(t)

	err := WritePartition(context.Background(), PartitionKey{"XAUUSD", RawH1, 2024, time.January}, nil, true)

	require.ErrorIs(t, err, ErrInstrumentOutOfScope)
	entries, rerr := os.ReadDir(root)
	require.NoError(t, rerr)
	assert.Empty(t, entries)
}

// Every interval, daily included, is partitioned by the UTC month of the bar's
// opening time, as the archive is (ADR-089). Daily bars open at 17:00
// America/New_York, so the last bar of a month opens on its last evening in UTC
// and belongs to that month's file, across month and DST boundaries.
func TestWritePartition_DailyBarsAreFiledByTheirUTCOpeningMonth(t *testing.T) {
	cases := []struct {
		name  string
		year  int
		month time.Month
		open  time.Time
	}{
		{"summer time, last bar of April", 2020, time.April, at(2020, time.April, 30, 21)},
		{"winter time, last bar of November", 2020, time.November, at(2020, time.November, 30, 22)},
		{"leap day, winter time", 2020, time.February, at(2020, time.February, 29, 22)},
		{"the Sunday the clocks go forward", 2020, time.March, at(2020, time.March, 8, 22)},
		{"the Sunday the clocks go back", 2020, time.November, at(2020, time.November, 1, 22)},
		{"first bar of a month by UTC", 2020, time.June, at(2020, time.June, 1, 21)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storetest.Root(t)
			key := PartitionKey{"EURUSD", RawD1, tc.year, tc.month}

			require.NoError(t, WritePartition(context.Background(), key, []Record{testRecord(tc.open, true)}, true))

			got, err := ReadPartitionRecords(context.Background(), key)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.True(t, got[0].Time.Equal(tc.open), "the provider's timestamp is kept verbatim")
			inv, err := Inspect(context.Background())
			require.NoError(t, err)
			assert.Equal(t, PartitionStatusOK, findPartition(t, inv, "EURUSD", RawD1, tc.year, tc.month).Status)
			assert.Empty(t, inv.Skipped)
		})
	}
}

// A row in the wrong month's file is a mis-assembled partition: the writer
// refuses it before touching the disk, for every interval, and an existing file
// is left as it was.
func TestWritePartition_RefusesARowOutsideItsPartitionMonth(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  PartitionKey
		open time.Time
	}{
		{"daily bar of April in the May file", PartitionKey{"EURUSD", RawD1, 2020, time.May}, at(2020, time.April, 30, 21)},
		{"hourly bar of the next month", PartitionKey{"EURUSD", RawH1, 2020, time.May}, at(2020, time.June, 1, 0)},
		{"same month, another year", PartitionKey{"EURUSD", RawH4, 2020, time.May}, at(2021, time.May, 3, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := storetest.Root(t)

			err := WritePartition(context.Background(), tc.key, []Record{testRecord(tc.open, true)}, true)

			require.ErrorIs(t, err, ErrMalformedData)
			entries, rerr := os.ReadDir(root)
			require.NoError(t, rerr)
			assert.Empty(t, entries, "nothing is created, not even the directories")
		})
	}
}

func TestUpdatePartition_ARowOutsideItsMonthLeavesTheExistingFileUntouched(t *testing.T) {
	root := storetest.Root(t)
	key := PartitionKey{"EURUSD", RawD1, 2020, time.May}
	require.NoError(t, WritePartition(context.Background(), key, []Record{testRecord(at(2020, time.May, 1, 21), true)}, true))
	path, err := key.Path()
	require.NoError(t, err)
	path = filepath.Join(root, path)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	err = UpdatePartition(context.Background(), key, func(existing []Record) ([]Record, error) {
		return append(existing, testRecord(at(2020, time.April, 30, 21), true)), nil
	})

	require.ErrorIs(t, err, ErrMalformedData)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestReadPartitionRecords_MissingPartitionReportsItsPath(t *testing.T) {
	root := storetest.Root(t)

	_, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})

	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.Contains(t, err.Error(), filepath.Join(root, "oanda", "raw", "EURUSD", "2024", "01"))
}

func TestUpdatePartition_CreatesAMissingPartitionFromTheMerge(t *testing.T) {
	storetest.Root(t)

	err := UpdatePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, func(existing []Record) ([]Record, error) {
		assert.Empty(t, existing)
		return []Record{testRecord(at(2024, 1, 1, 0), true)}, nil
	})

	require.NoError(t, err)
	got, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestUpdatePartition_ExtendsFromTheExistingRecords(t *testing.T) {
	storetest.Root(t)
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, []Record{testRecord(at(2024, 1, 1, 0), true)}, true))

	err := UpdatePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, func(existing []Record) ([]Record, error) {
		require.Len(t, existing, 1)
		return append(existing, testRecord(at(2024, 1, 1, 1), true)), nil
	})

	require.NoError(t, err)
	got, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestUpdatePartition_MergeErrorLeavesTheFileUntouched(t *testing.T) {
	storetest.Root(t)
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, []Record{testRecord(at(2024, 1, 1, 0), true)}, true))
	before, err := FingerprintPartition(PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	boom := errors.New("boom")

	err = UpdatePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, func([]Record) ([]Record, error) { return nil, boom })

	require.ErrorIs(t, err, boom)
	after, err := FingerprintPartition(PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestUpdatePartition_ReportsAMalformedExistingFileAndDoesNotReplaceIt(t *testing.T) {
	root := storetest.Root(t)
	path, err := PartitionKey{"EURUSD", RawH1, 2024, time.January}.Path()
	require.NoError(t, err)
	path = filepath.Join(root, path)
	writeFile(t, filepath.Dir(path), filepath.Base(path), "garbage\n")
	called := false

	err = UpdatePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, func(e []Record) ([]Record, error) {
		called = true
		return e, nil
	})

	require.ErrorIs(t, err, ErrMalformedData)
	assert.False(t, called)
	data, rerr := os.ReadFile(path)
	require.NoError(t, rerr)
	assert.Equal(t, "garbage\n", string(data))
}

func TestUpdatePartition_RejectsOutOfScopeAndCancelledInput(t *testing.T) {
	storetest.Root(t)
	merge := func(e []Record) ([]Record, error) { return e, nil }

	err := UpdatePartition(context.Background(), PartitionKey{"XAUUSD", RawH1, 2024, time.January}, merge)
	assert.ErrorIs(t, err, ErrInstrumentOutOfScope)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = UpdatePartition(ctx, PartitionKey{"EURUSD", RawH1, 2024, time.January}, merge)
	assert.ErrorIs(t, err, context.Canceled)
}

// Two overlapping extends of one partition must both land: each starts from
// the other's result, so no writer's records are lost.
func TestUpdatePartition_ConcurrentExtendsLoseNothing(t *testing.T) {
	root := storetest.Root(t)
	const writers = 24
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := UpdatePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, func(existing []Record) ([]Record, error) {
				return append(existing, testRecord(at(2024, 1, 1, i), true)), nil
			})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	got, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, writers, "every writer's record survives")
	entries, err := os.ReadDir(filepath.Dir(mustPath(t, root)))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left behind")
}

// With two records, a replacing WritePartition checks ctx five times before it
// commits: on entry, once per record while encoding, on entry to the store's
// write, and at the commit point. Cancelling at the fifth check leaves the old
// file; cancelling after it cannot undo the replacement.
func TestWritePartition_CancellationAtAndAfterTheCommitPoint(t *testing.T) {
	storetest.Root(t)
	first := []Record{testRecord(at(2024, 1, 1, 0), true)}
	both := append(append([]Record(nil), first...), testRecord(at(2024, 1, 1, 1), true))
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, first, true))

	atCommit := &nthCancelContext{Context: context.Background(), remaining: 4}
	err := WritePartition(atCommit, PartitionKey{"EURUSD", RawH1, 2024, time.January}, both, false)
	require.ErrorIs(t, err, context.Canceled)
	got, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, 1, "cancelled at the commit point: the old file stands")

	afterCommit := &nthCancelContext{Context: context.Background(), remaining: 5}
	require.NoError(t, WritePartition(afterCommit, PartitionKey{"EURUSD", RawH1, 2024, time.January}, both, false))
	require.ErrorIs(t, afterCommit.Err(), context.Canceled, "the context is cancelled once the commit is done")
	got, err = ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, 2, "cancelled after the commit: the replacement is not undone")
}

func TestWritePartition_PriceTooPreciseWritesNothing(t *testing.T) {
	root := storetest.Root(t)
	rec := testRecord(at(2024, 1, 1, 0), true)
	rec.BidOpen = num.MustParsePrice("1.123456")

	err := WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, []Record{rec}, true)

	require.ErrorIs(t, err, ErrMalformedData)
	_, serr := os.Stat(mustPath(t, root))
	assert.ErrorIs(t, serr, fs.ErrNotExist)
}

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
	storetest.Root(t)
	records := []Record{
		testRecord(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), true),
		testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true), // unsorted on purpose
	}

	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, records, true))

	got, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	require.Len(t, got, 2)
	// Sorted ascending regardless of input order.
	assert.True(t, got[0].Time.Before(got[1].Time))
	assert.True(t, got[0].Time.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, records[0].BidOpen.String(), got[0].BidOpen.String())
}

func TestWritePartition_MustNotExistRejectsExisting(t *testing.T) {
	storetest.Root(t)
	records := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, records, true))

	err := WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, records, true)
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
	storetest.Root(t)
	const n = 8

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			records := []Record{testRecord(time.Date(2024, 1, 1, i, 0, 0, 0, time.UTC), true)}
			errs[i] = WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, records, true)
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

	got, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	require.Len(t, got, 1, "the file must hold exactly one writer's content, not a mix")
}

func TestWritePartition_ExtendReplacesExisting(t *testing.T) {
	storetest.Root(t)
	first := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, first, true))

	merged := append(first, testRecord(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), true))
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, merged, false))

	got, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestReadPartitionRecords_MissingFile(t *testing.T) {
	storetest.Root(t)
	_, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	assert.True(t, errors.Is(err, fs.ErrNotExist), "want fs.ErrNotExist, got %v", err)
}

func TestWritePartition_EmptyRecordsProducesValidHeaderOnlyFile(t *testing.T) {
	storetest.Root(t)
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, nil, true))
	got, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestWritePartition_PathMatchesArchiveLayout(t *testing.T) {
	root := storetest.Root(t)
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawD1, 2024, time.March}, []Record{
		testRecord(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), true),
	}, true))

	want := root + "/oanda/raw/EURUSD/2024/03/EURUSD-2024-03-d1.csv"
	_, err := os.Stat(want)
	require.NoError(t, err, "expected file at %s", want)

	// And Inspect (the archive-wide inventory walker) must recognize it.
	inv, err := Inspect(context.Background())
	require.NoError(t, err)
	p := findPartition(t, inv, "EURUSD", RawD1, 2024, time.March)
	assert.Equal(t, PartitionStatusOK, p.Status)
	assert.Equal(t, eurusdID(), p.Instrument)
}

// nthCancelContext reports context.Canceled from Err() once its
// remaining count is exhausted, mirroring marketdata/store_csv_test.go's
// own helper (unexported per-package, so duplicated here rather than
// shared across the provider/oanda boundary).
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
	storetest.Root(t)
	original := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, original, true))

	ctx := &nthCancelContext{Context: context.Background(), remaining: 0}
	err := WritePartition(ctx, PartitionKey{"EURUSD", RawH1, 2024, time.January}, append(original, testRecord(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), true)), false)
	assert.ErrorIs(t, err, context.Canceled)

	got, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	require.Len(t, got, 1, "original file must be untouched")
}

func TestWritePartition_CancelledJustBeforeRenameLeavesExistingFileIntact(t *testing.T) {
	storetest.Root(t)
	original := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, original, true))

	// remaining=2: the initial ctx.Err() check, then one per-record
	// check during encode (one record) both succeed; cancellation lands
	// on the commit-point check immediately before the rename.
	ctx := &nthCancelContext{Context: context.Background(), remaining: 2}
	err := WritePartition(ctx, PartitionKey{"EURUSD", RawH1, 2024, time.January}, append(original, testRecord(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), true)), false)
	assert.ErrorIs(t, err, context.Canceled)

	got, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	require.Len(t, got, 1, "original file must still be intact; cancelled write must not have replaced it")
}

func TestFingerprintPartition(t *testing.T) {
	storetest.Root(t)
	records := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, records, true))

	got, err := FingerprintPartition(PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Regexp(t, "^sha256:[0-9a-f]{64}$", got)

	// Must match Inspect's own fingerprint for the identical file.
	inv, err := Inspect(context.Background())
	require.NoError(t, err)
	p := findPartition(t, inv, "EURUSD", RawH1, 2024, time.January)
	assert.Equal(t, p.Fingerprint, got)
}

func TestFingerprintPartition_MissingFile(t *testing.T) {
	storetest.Root(t)
	_, err := FingerprintPartition(PartitionKey{"EURUSD", RawH1, 2024, time.January})
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestReadPartitionSnapshot_RecordsAndFingerprintAgree(t *testing.T) {
	storetest.Root(t)
	records := []Record{
		testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true),
		testRecord(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), true),
	}
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, records, true))

	snap, err := ReadPartitionSnapshot(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	require.Len(t, snap.Records, 2)

	wantFingerprint, err := FingerprintPartition(PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Equal(t, wantFingerprint, snap.Fingerprint)

	wantRecords, err := ReadPartitionRecords(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	require.Len(t, wantRecords, len(snap.Records))
	for i := range wantRecords {
		assert.True(t, wantRecords[i].Time.Equal(snap.Records[i].Time))
	}
}

func TestReadPartitionSnapshot_MissingFile(t *testing.T) {
	storetest.Root(t)
	_, err := ReadPartitionSnapshot(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

// TestReadPartitionSnapshot_ConsistentUnderConcurrentReplace is the
// regression for a design-review finding on issue #81's PR: calling
// ReadPartitionRecords and FingerprintPartition as two separate file
// opens (the pattern this replaces) admits a window in which a
// concurrent WritePartition "extend" replaces the file in between,
// pairing one revision's records with another revision's fingerprint.
// One goroutine repeatedly replaces the partition with one of two
// distinguishable, precomputed contents (contentA/contentB, differing
// row counts and hence differing fingerprints); another repeatedly
// calls ReadPartitionSnapshot. Every single snapshot observed must pair
// a row count with the fingerprint that actually corresponds to it —
// under the old two-open pattern this test would (non-deterministically,
// but reliably across enough iterations) observe a snapshot whose row
// count matches one content variant and whose fingerprint matches the
// other.
func TestReadPartitionSnapshot_ConsistentUnderConcurrentReplace(t *testing.T) {
	storetest.Root(t)
	contentA := []Record{testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)}
	contentB := []Record{
		testRecord(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true),
		testRecord(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), true),
		testRecord(time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC), true),
	}
	require.NoError(t, WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, contentA, true))

	fpFor := func(records []Record) string {
		data, err := encodePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, records)
		require.NoError(t, err)
		return fingerprintBytes(data)
	}
	fpA, fpB := fpFor(contentA), fpFor(contentB)
	require.NotEqual(t, fpA, fpB, "fixture assumption: the two contents must fingerprint differently")

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range iterations {
			content := contentA
			if i%2 == 0 {
				content = contentB
			}
			_ = WritePartition(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January}, content, false)
		}
	}()

	var mismatches int
	go func() {
		defer wg.Done()
		for range iterations {
			snap, err := ReadPartitionSnapshot(context.Background(), PartitionKey{"EURUSD", RawH1, 2024, time.January})
			if err != nil {
				continue // a rename mid-open is a legitimate transient read error, not the hazard under test
			}
			switch {
			case len(snap.Records) == len(contentA) && snap.Fingerprint != fpA:
				mismatches++
			case len(snap.Records) == len(contentB) && snap.Fingerprint != fpB:
				mismatches++
			}
		}
	}()
	wg.Wait()

	assert.Equal(t, 0, mismatches, "every snapshot's record count and fingerprint must describe the same file revision")
}
