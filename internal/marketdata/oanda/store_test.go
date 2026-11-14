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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/marketdata/layout"
	"github.com/rustyeddy/trader/num"
)

func at(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

// fixtureCase names an archive fixture and the partition it describes.
type fixtureCase struct {
	file     string
	symbol   string
	interval RawInterval
	year     int
	month    time.Month
}

var fixtures = []fixtureCase{
	{"EURUSD-2020-05-h1.csv", "EURUSD", RawH1, 2020, time.May},
	{"EURUSD-2020-05-d1.csv", "EURUSD", RawD1, 2020, time.May},
	{"USDJPY-2020-05-h1.csv", "USDJPY", RawH1, 2020, time.May},
}

// A file the writer produces from a real archive file's records is
// byte-identical to that file, including the daily file's tf=d header.
func TestWritePartition_ReproducesArchiveFixturesByteForByte(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.file, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", f.file))
			require.NoError(t, err)
			_, records, err := ReadFile(context.Background(), filepath.Join("testdata", f.file))
			require.NoError(t, err)
			root := t.TempDir()

			require.NoError(t, WritePartition(context.Background(), root, PartitionKey{f.symbol, f.interval, f.year, f.month}, records, true))

			path, err := PartitionKey{f.symbol, f.interval, f.year, f.month}.Path(root)
			require.NoError(t, err)
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

func TestPartitionKeyPath_UsesTheLayoutResolver(t *testing.T) {
	root := t.TempDir()

	got, err := PartitionKey{"EURUSD", RawH4, 2024, time.July}.Path(root)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "oanda", "raw", "EURUSD", "2024", "07", "EURUSD-2024-07-h4.csv"), got)
	p, err := layout.Parse(root, got)
	require.NoError(t, err)
	assert.Equal(t, layout.Partition{Source: "oanda", Stage: layout.Raw, Instrument: "EURUSD", Year: 2024, Month: time.July, Interval: "h4"}, p)
}

func TestPartitionKeyPath_RejectsWhatIsNotInTheCorpus(t *testing.T) {
	root := t.TempDir()
	for name, tc := range map[string]struct {
		symbol   string
		interval RawInterval
		year     int
		month    time.Month
		want     error
	}{
		"gold":            {"XAUUSD", RawH1, 2024, 1, ErrInstrumentOutOfScope},
		"traversal":       {"../EURUSD", RawH1, 2024, 1, ErrMalformedData},
		"lower case":      {"eurusd", RawH1, 2024, 1, ErrMalformedData},
		"weekly":          {"EURUSD", RawInterval("w1"), 2024, 1, ErrUnsupportedInterval},
		"daily spelled d": {"EURUSD", RawInterval("d"), 2024, 1, ErrUnsupportedInterval},
		"upper interval":  {"EURUSD", RawInterval("H1"), 2024, 1, ErrUnsupportedInterval},
		"month zero":      {"EURUSD", RawH1, 2024, 0, layout.ErrInvalidComponent},
		"month 13":        {"EURUSD", RawH1, 2024, 13, layout.ErrInvalidComponent},
		"year 99":         {"EURUSD", RawH1, 99, 1, layout.ErrInvalidComponent},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := PartitionKey{tc.symbol, tc.interval, tc.year, tc.month}.Path(root)

			assert.ErrorIs(t, err, tc.want)
		})
	}
	_, err := PartitionKey{"EURUSD", RawH1, 2024, 1}.Path("")
	assert.ErrorIs(t, err, layout.ErrInvalidRoot)
}

func TestWritePartition_RefusesAnOutOfScopeSymbolAndWritesNothing(t *testing.T) {
	root := t.TempDir()

	err := WritePartition(context.Background(), root, PartitionKey{"XAUUSD", RawH1, 2024, time.January}, nil, true)

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
			root := t.TempDir()
			key := PartitionKey{"EURUSD", RawD1, tc.year, tc.month}

			require.NoError(t, WritePartition(context.Background(), root, key, []Record{testRecord(tc.open, true)}, true))

			got, err := ReadPartitionRecords(context.Background(), root, key)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.True(t, got[0].Time.Equal(tc.open), "the provider's timestamp is kept verbatim")
			inv, err := Inspect(context.Background(), root)
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
			root := t.TempDir()

			err := WritePartition(context.Background(), root, tc.key, []Record{testRecord(tc.open, true)}, true)

			require.ErrorIs(t, err, ErrMalformedData)
			entries, rerr := os.ReadDir(root)
			require.NoError(t, rerr)
			assert.Empty(t, entries, "nothing is created, not even the directories")
		})
	}
}

func TestUpdatePartition_ARowOutsideItsMonthLeavesTheExistingFileUntouched(t *testing.T) {
	root := t.TempDir()
	key := PartitionKey{"EURUSD", RawD1, 2020, time.May}
	require.NoError(t, WritePartition(context.Background(), root, key, []Record{testRecord(at(2020, time.May, 1, 21), true)}, true))
	path, err := key.Path(root)
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	err = UpdatePartition(context.Background(), root, key, func(existing []Record) ([]Record, error) {
		return append(existing, testRecord(at(2020, time.April, 30, 21), true)), nil
	})

	require.ErrorIs(t, err, ErrMalformedData)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestReader_RejectsARowOutsideItsPartitionMonth(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "oanda", "raw", "EURUSD", "2020", "05")
	writeFile(t, dir, "EURUSD-2020-05-h1.csv", fmtHeader("EURUSD", 2020, 5)+h1Row(at(2020, 5, 1, 0), true)+h1Row(at(2020, 6, 1, 0), true))

	_, err := ReadPartitionRecords(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2020, time.May})

	require.ErrorIs(t, err, ErrMalformedData)
	assert.Contains(t, err.Error(), "2020-06-01T00:00:00Z")
}

func TestInspect_DoesNotReadTheLegacyLayout(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "raw", "oanda", "EURUSD", "2020", "05")
	writeFile(t, legacy, "EURUSD-2020-05-h1.csv", fmtHeader("EURUSD", 2020, 5)+h1Row(at(2020, 5, 1, 0), true))

	_, err := Inspect(context.Background(), root)

	require.Error(t, err, "no oanda/raw directory exists: the legacy tree is not a fallback")
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.Contains(t, err.Error(), filepath.Join(root, "oanda", "raw"), "the path that was looked up is reported")
}

func TestInspect_RejectsAnEmptyRoot(t *testing.T) {
	_, err := Inspect(context.Background(), "")

	assert.ErrorIs(t, err, layout.ErrInvalidRoot)
}

func TestReadPartitionRecords_MissingPartitionReportsItsPath(t *testing.T) {
	root := t.TempDir()

	_, err := ReadPartitionRecords(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January})

	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.Contains(t, err.Error(), filepath.Join(root, "oanda", "raw", "EURUSD", "2024", "01"))
}

// --- UpdatePartition and the partition lock ---

func TestUpdatePartition_CreatesAMissingPartitionFromTheMerge(t *testing.T) {
	root := t.TempDir()

	err := UpdatePartition(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, func(existing []Record) ([]Record, error) {
		assert.Empty(t, existing)
		return []Record{testRecord(at(2024, 1, 1, 0), true)}, nil
	})

	require.NoError(t, err)
	got, err := ReadPartitionRecords(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestUpdatePartition_ExtendsFromTheExistingRecords(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, WritePartition(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, []Record{testRecord(at(2024, 1, 1, 0), true)}, true))

	err := UpdatePartition(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, func(existing []Record) ([]Record, error) {
		require.Len(t, existing, 1)
		return append(existing, testRecord(at(2024, 1, 1, 1), true)), nil
	})

	require.NoError(t, err)
	got, err := ReadPartitionRecords(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestUpdatePartition_MergeErrorLeavesTheFileUntouched(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, WritePartition(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, []Record{testRecord(at(2024, 1, 1, 0), true)}, true))
	before, err := FingerprintPartition(root, PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	boom := errors.New("boom")

	err = UpdatePartition(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, func([]Record) ([]Record, error) { return nil, boom })

	require.ErrorIs(t, err, boom)
	after, err := FingerprintPartition(root, PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestUpdatePartition_ReportsAMalformedExistingFileAndDoesNotReplaceIt(t *testing.T) {
	root := t.TempDir()
	path, err := PartitionKey{"EURUSD", RawH1, 2024, time.January}.Path(root)
	require.NoError(t, err)
	writeFile(t, filepath.Dir(path), filepath.Base(path), "garbage\n")
	called := false

	err = UpdatePartition(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, func(e []Record) ([]Record, error) {
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
	root := t.TempDir()
	merge := func(e []Record) ([]Record, error) { return e, nil }

	err := UpdatePartition(context.Background(), root, PartitionKey{"XAUUSD", RawH1, 2024, time.January}, merge)
	assert.ErrorIs(t, err, ErrInstrumentOutOfScope)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = UpdatePartition(ctx, root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, merge)
	assert.ErrorIs(t, err, context.Canceled)
}

// Two overlapping extends of one partition must both land: each starts from
// the other's result, so no writer's records are lost.
func TestUpdatePartition_ConcurrentExtendsLoseNothing(t *testing.T) {
	root := t.TempDir()
	const writers = 24
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := UpdatePartition(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, func(existing []Record) ([]Record, error) {
				return append(existing, testRecord(at(2024, 1, 1, i), true)), nil
			})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	got, err := ReadPartitionRecords(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, writers, "every writer's record survives")
	entries, err := os.ReadDir(filepath.Dir(mustPath(t, root)))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left behind")
	assert.Empty(t, partitionLocks.m, "the lock registry does not retain finished partitions")
}

func mustPath(t *testing.T, root string) string {
	t.Helper()
	p, err := PartitionKey{"EURUSD", RawH1, 2024, time.January}.Path(root)
	require.NoError(t, err)
	return p
}

func TestLockPartition_WaiterHonorsCancellation(t *testing.T) {
	unlock, err := lockPartition(context.Background(), "p")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := lockPartition(ctx, "p")
		done <- err
	}()

	cancel()

	assert.ErrorIs(t, <-done, context.Canceled)
	unlock()
	partitionLocks.mu.Lock()
	defer partitionLocks.mu.Unlock()
	assert.Empty(t, partitionLocks.m)
}

func TestLockPartition_DifferentPartitionsDoNotBlockEachOther(t *testing.T) {
	a, err := lockPartition(context.Background(), "a")
	require.NoError(t, err)
	defer a()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	b, err := lockPartition(ctx, "b")

	require.NoError(t, err)
	b()
}

// --- The commit point ---

// With two records, WritePartition checks ctx four times before it commits: on
// entry, once per record while encoding, and at the commit point. Cancelling at
// the fourth check leaves the old file; cancelling after it cannot undo the
// replacement.
func TestWritePartition_CancellationAtAndAfterTheCommitPoint(t *testing.T) {
	root := t.TempDir()
	first := []Record{testRecord(at(2024, 1, 1, 0), true)}
	both := append(append([]Record(nil), first...), testRecord(at(2024, 1, 1, 1), true))
	require.NoError(t, WritePartition(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, first, true))

	atCommit := &nthCancelContext{Context: context.Background(), remaining: 3}
	err := WritePartition(atCommit, root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, both, false)
	require.ErrorIs(t, err, context.Canceled)
	got, err := ReadPartitionRecords(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, 1, "cancelled at the commit point: the old file stands")

	afterCommit := &nthCancelContext{Context: context.Background(), remaining: 4}
	require.NoError(t, WritePartition(afterCommit, root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, both, false))
	require.ErrorIs(t, afterCommit.Err(), context.Canceled, "the context is cancelled once the commit is done")
	got, err = ReadPartitionRecords(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, 2, "cancelled after the commit: the replacement is not undone")
}

func TestFormatRawPrice(t *testing.T) {
	for in, want := range map[string]string{
		"1.0805":    "1.08050",
		"107.272":   "107.27200",
		"107.27200": "107.27200",
		"1":         "1.00000",
		"0":         "0.00000",
		"1.08107":   "1.08107",
	} {
		got, err := formatRawPrice(num.MustParsePrice(in))

		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}

func TestFormatRawPrice_RefusesToRound(t *testing.T) {
	_, err := formatRawPrice(num.MustParsePrice("1.123456"))

	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestWritePartition_PriceTooPreciseWritesNothing(t *testing.T) {
	root := t.TempDir()
	rec := testRecord(at(2024, 1, 1, 0), true)
	rec.BidOpen = num.MustParsePrice("1.123456")

	err := WritePartition(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, []Record{rec}, true)

	require.ErrorIs(t, err, ErrMalformedData)
	_, serr := os.Stat(mustPath(t, root))
	assert.ErrorIs(t, serr, fs.ErrNotExist)
}

// One root spelled relative and absolute is one partition: both writers share a
// lock and neither loses the other's records.
func TestUpdatePartition_RelativeAndAbsoluteRootsShareALock(t *testing.T) {
	t.Chdir(t.TempDir())
	abs, err := filepath.Abs("data")
	require.NoError(t, err)
	roots := []string{"data", abs, "./data", abs + "/"}
	const perRoot = 8
	var wg sync.WaitGroup
	for r, root := range roots {
		for i := range perRoot {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := UpdatePartition(context.Background(), root, PartitionKey{"EURUSD", RawH1, 2024, time.January}, func(existing []Record) ([]Record, error) {
					return append(existing, testRecord(at(2024, 1, 1, r*perRoot+i), true)), nil
				})
				assert.NoError(t, err)
			}()
		}
	}
	wg.Wait()

	got, err := ReadPartitionRecords(context.Background(), abs, PartitionKey{"EURUSD", RawH1, 2024, time.January})
	require.NoError(t, err)
	assert.Len(t, got, len(roots)*perRoot)
}

func TestPartitionKeyValidate(t *testing.T) {
	assert.NoError(t, PartitionKey{"EURUSD", RawD1, 2024, time.December}.Validate())
	assert.ErrorIs(t, PartitionKey{"XAUUSD", RawH1, 2024, 1}.Validate(), ErrInstrumentOutOfScope)
	assert.ErrorIs(t, PartitionKey{"EURUSD", RawInterval("w1"), 2024, 1}.Validate(), ErrUnsupportedInterval)
	assert.ErrorIs(t, PartitionKey{"EURUSD", RawH1, 2024, 13}.Validate(), layout.ErrInvalidComponent)
	assert.ErrorIs(t, PartitionKey{}.Validate(), ErrMalformedData, "the zero key names nothing")
}

func TestMetaKeyRoundTripsThroughThePath(t *testing.T) {
	root := t.TempDir()
	for _, f := range fixtures {
		meta, _, err := parsePathMeta(filepath.Join("testdata", f.file))
		require.NoError(t, err)

		key := meta.Key()

		assert.Equal(t, PartitionKey{f.symbol, f.interval, f.year, f.month}, key, f.file)
		path, err := key.Path(root)
		require.NoError(t, err)
		assert.Equal(t, f.file, filepath.Base(path), "the file name is the fixture's name")
	}
}
