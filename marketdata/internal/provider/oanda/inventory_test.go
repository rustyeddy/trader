package oanda

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile creates dir/name (creating dir if needed) with content, and
// returns the full path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func h1Row(t time.Time, complete bool) string {
	return t.Format(time.RFC3339) + ",1.10000,1.10100,1.09900,1.10050,1.10010,1.10110,1.09910,1.10060,100," +
		map[bool]string{true: "true", false: "false"}[complete] + "\n"
}

// buildArchive lays out a small synthetic raw archive under root:
//
//   - EURUSD/2020/05: two rows, one duplicate timestamp, one incomplete
//   - EURUSD/2020/06: one row (ordinary)
//   - EURUSD/2020/08: one row (2020/07 is deliberately absent -> a gap)
//   - USDJPY/2020/05: malformed (bad column header)
//   - XAUUSD/2020/05: out of scope, must be skipped
//   - EURUSD/2020/05: a stray w1 file, unsupported interval, must be skipped
//   - a garbage file name that isn't PAIR-YYYY-MM-tf.csv at all
func buildArchive(t *testing.T, root string) {
	t.Helper()

	eurusdMay := root + "/EURUSD/2020/05"
	writeFile(t, eurusdMay, "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+
			h1Row(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), true)+
			h1Row(time.Date(2020, 5, 1, 1, 0, 0, 0, time.UTC), false)+
			h1Row(time.Date(2020, 5, 1, 1, 0, 0, 0, time.UTC), true), // duplicate of the row above
	)
	writeFile(t, eurusdMay, "EURUSD-2020-05-w1.csv",
		fmtHeaderTF("EURUSD", "w1", 2020, 5)+h1Row(time.Date(2020, 5, 3, 0, 0, 0, 0, time.UTC), true))

	eurusdJune := root + "/EURUSD/2020/06"
	writeFile(t, eurusdJune, "EURUSD-2020-06-h1.csv",
		fmtHeader("EURUSD", 2020, 6)+h1Row(time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC), true))

	// 2020/07 intentionally absent: EURUSD/H1's gap.
	eurusdAug := root + "/EURUSD/2020/08"
	writeFile(t, eurusdAug, "EURUSD-2020-08-h1.csv",
		fmtHeader("EURUSD", 2020, 8)+h1Row(time.Date(2020, 8, 1, 0, 0, 0, 0, time.UTC), true))

	usdjpyMay := root + "/USDJPY/2020/05"
	writeFile(t, usdjpyMay, "USDJPY-2020-05-h1.csv",
		"# schema=raw-v1 source=oanda instrument=USDJPY tf=h1 year=2020 month=05\nnot,the,right,header\n"+
			h1Row(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), true))

	xauusdMay := root + "/XAUUSD/2020/05"
	writeFile(t, xauusdMay, "XAUUSD-2020-05-h1.csv",
		fmtHeader("XAUUSD", 2020, 5)+h1Row(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), true))

	writeFile(t, root, "not-a-partition-file.csv", "garbage\n")
}

func fmtHeader(symbol string, year, month int) string {
	return fmtHeaderTF(symbol, "h1", year, month)
}

func fmtHeaderTF(symbol, tf string, year, month int) string {
	return fmt.Sprintf("# schema=raw-v1 source=oanda instrument=%s tf=%s year=%02d month=%02d\n"+
		"time,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume,complete\n",
		symbol, tf, year, month)
}

func eurusdID() instrument.ID {
	return instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
}

func findPartition(t *testing.T, inv Inventory, symbol string, interval RawInterval, year int, month time.Month) Partition {
	t.Helper()
	for _, p := range inv.Partitions {
		if p.Symbol == symbol && p.Interval == interval && p.Year == year && p.Month == month {
			return p
		}
	}
	t.Fatalf("no partition found for %s %s %d-%02d", symbol, interval, year, int(month))
	return Partition{}
}

func TestInspect_OKPartitionSummary(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	p := findPartition(t, inv, "EURUSD", RawH1, 2020, time.May)
	assert.Equal(t, eurusdID(), p.Instrument)
	assert.Equal(t, PartitionStatusOK, p.Status)
	assert.NoError(t, p.Err)
	assert.Equal(t, 3, p.RowCount)
	assert.Equal(t, 1, p.IncompleteCount, "the false-complete row")
	require.Len(t, p.DuplicateTimes, 1)
	assert.Equal(t, time.Date(2020, 5, 1, 1, 0, 0, 0, time.UTC), p.DuplicateTimes[0])
	assert.Equal(t, time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), p.FirstTime)
	assert.Equal(t, time.Date(2020, 5, 1, 1, 0, 0, 0, time.UTC), p.LastTime)
	assert.NotEmpty(t, p.Fingerprint)
	assert.Regexp(t, "^sha256:[0-9a-f]{64}$", p.Fingerprint)
}

func TestInspect_FingerprintDeterministicAndContentSensitive(t *testing.T) {
	root1, root2 := t.TempDir(), t.TempDir()
	buildArchive(t, root1)
	buildArchive(t, root2)

	inv1, err := Inspect(context.Background(), root1)
	require.NoError(t, err)
	inv2, err := Inspect(context.Background(), root2)
	require.NoError(t, err)

	p1 := findPartition(t, inv1, "EURUSD", RawH1, 2020, time.June)
	p2 := findPartition(t, inv2, "EURUSD", RawH1, 2020, time.June)
	assert.Equal(t, p1.Fingerprint, p2.Fingerprint, "identical bytes must fingerprint identically")

	pMay := findPartition(t, inv1, "EURUSD", RawH1, 2020, time.May)
	assert.NotEqual(t, pMay.Fingerprint, p1.Fingerprint, "different content must fingerprint differently")
}

func TestInspect_MalformedPartition(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	p := findPartition(t, inv, "USDJPY", RawH1, 2020, time.May)
	assert.Equal(t, PartitionStatusMalformed, p.Status)
	assert.ErrorIs(t, p.Err, ErrMalformedData)
	assert.NotEmpty(t, p.Fingerprint, "bytes are still hashable even if rows don't parse")
	assert.Zero(t, p.RowCount)
}

func TestInspect_SkipsOutOfScopeInstrument(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	for _, p := range inv.Partitions {
		assert.NotEqual(t, "XAUUSD", p.Symbol, "XAUUSD must never become a Partition")
	}

	var found *SkippedEntry
	for i, s := range inv.Skipped {
		if filepath.Base(s.Path) == "XAUUSD-2020-05-h1.csv" {
			found = &inv.Skipped[i]
		}
	}
	require.NotNil(t, found, "XAUUSD must be explicitly reported as skipped")
	assert.ErrorIs(t, found.Reason, ErrInstrumentOutOfScope)
}

func TestInspect_SkipsUnsupportedInterval(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	var found *SkippedEntry
	for i, s := range inv.Skipped {
		if filepath.Base(s.Path) == "EURUSD-2020-05-w1.csv" {
			found = &inv.Skipped[i]
		}
	}
	require.NotNil(t, found)
	assert.ErrorIs(t, found.Reason, ErrUnsupportedInterval)
}

func TestInspect_SkipsUnrecognizedFileName(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	var found *SkippedEntry
	for i, s := range inv.Skipped {
		if filepath.Base(s.Path) == "not-a-partition-file.csv" {
			found = &inv.Skipped[i]
		}
	}
	require.NotNil(t, found)
	assert.ErrorIs(t, found.Reason, ErrMalformedData)
}

func TestInspect_MonthGap(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	require.Len(t, inv.Gaps, 1)
	gap := inv.Gaps[0]
	assert.Equal(t, "EURUSD", gap.Symbol)
	assert.Equal(t, RawH1, gap.Interval)
	assert.Equal(t, 2020, gap.Year)
	assert.Equal(t, time.July, gap.Month)
}

func TestInspect_NoGapsForSingleMonthPair(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	for _, g := range inv.Gaps {
		assert.NotEqual(t, "USDJPY", g.Symbol, "a single-month pair has no interior months to gap")
	}
}

func TestInspect_Deterministic(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	inv1, err := Inspect(context.Background(), root)
	require.NoError(t, err)
	inv2, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	assert.Equal(t, inv1, inv2)
}

func TestInspect_PartitionsSortedDeterministically(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	for i := 1; i < len(inv.Partitions); i++ {
		a, b := inv.Partitions[i-1], inv.Partitions[i]
		key := func(p Partition) string {
			return fmt.Sprintf("%s|%s|%04d|%02d", p.Symbol, p.Interval, p.Year, int(p.Month))
		}
		assert.LessOrEqual(t, key(a), key(b))
	}
}

func TestInspect_NeverModifiesSourceFiles(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)
	path := filepath.Join(root, "EURUSD", "2020", "05", "EURUSD-2020-05-h1.csv")
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	_, err = Inspect(context.Background(), root)
	require.NoError(t, err)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestInspect_RootNotFound(t *testing.T) {
	_, err := Inspect(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"))
	assert.Error(t, err)
}

func TestInspect_ContextCancellation(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Inspect(ctx, root)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPartitionStatus_String(t *testing.T) {
	cases := map[PartitionStatus]string{
		PartitionStatusUnknown:    "unknown",
		PartitionStatusOK:         "ok",
		PartitionStatusUnreadable: "unreadable",
		PartitionStatusMalformed:  "malformed",
		PartitionStatus(200):      "PartitionStatus(200)",
	}
	for status, want := range cases {
		assert.Equal(t, want, status.String())
	}
}

func TestInspect_UnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions cannot make a file unreadable")
	}
	root := t.TempDir()
	dir := root + "/EURUSD/2020/05"
	path := writeFile(t, dir, "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+h1Row(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), true))
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	p := findPartition(t, inv, "EURUSD", RawH1, 2020, time.May)
	assert.Equal(t, PartitionStatusUnreadable, p.Status)
	assert.Error(t, p.Err)
	assert.Empty(t, p.Fingerprint)
}

func TestMonthKey_NextWrapsYear(t *testing.T) {
	k := monthKey{year: 2020, month: time.December}
	assert.Equal(t, monthKey{year: 2021, month: time.January}, k.next())
}

func TestMonthKey_Before(t *testing.T) {
	assert.True(t, monthKey{2020, time.May}.before(monthKey{2020, time.June}))
	assert.True(t, monthKey{2020, time.December}.before(monthKey{2021, time.January}))
	assert.False(t, monthKey{2020, time.June}.before(monthKey{2020, time.June}))
}

// Sanity check that errors.Is works through SkippedEntry.Reason the way
// callers are expected to use it.
func TestSkippedEntry_ReasonClassifiable(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)
	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	var sawScope, sawInterval, sawMalformed bool
	for _, s := range inv.Skipped {
		switch {
		case errors.Is(s.Reason, ErrInstrumentOutOfScope):
			sawScope = true
		case errors.Is(s.Reason, ErrUnsupportedInterval):
			sawInterval = true
		case errors.Is(s.Reason, ErrMalformedData):
			sawMalformed = true
		}
	}
	assert.True(t, sawScope)
	assert.True(t, sawInterval)
	assert.True(t, sawMalformed)
}

// buildTieBreakArchive exercises sort tie-break branches that
// buildArchive's fixture doesn't reach: two entries sharing a Symbol but
// differing in Interval, and gaps sharing a Symbol/Interval but
// differing in Year.
func buildTieBreakArchive(t *testing.T, root string) {
	t.Helper()

	// EURUSD H1: present 2020/05, 2020/07 (gap at 06) and 2021/01,
	// 2021/03 (gap at 02) -- two H1 gaps, same symbol+interval,
	// different year.
	for _, ym := range [][2]int{{2020, 5}, {2020, 7}, {2021, 1}, {2021, 3}} {
		dir := fmt.Sprintf("%s/EURUSD/%04d/%02d", root, ym[0], ym[1])
		writeFile(t, dir, fmt.Sprintf("EURUSD-%04d-%02d-h1.csv", ym[0], ym[1]),
			fmtHeader("EURUSD", ym[0], ym[1])+h1Row(time.Date(ym[0], time.Month(ym[1]), 1, 0, 0, 0, 0, time.UTC), true))
	}

	// EURUSD H4: present 2020/05, 2020/07 (gap at 06) -- same symbol as
	// above, different interval.
	for _, month := range []int{5, 7} {
		dir := fmt.Sprintf("%s/EURUSD/2020/%02d", root, month)
		writeFile(t, dir, fmt.Sprintf("EURUSD-2020-%02d-h4.csv", month),
			fmtHeaderTF("EURUSD", "h4", 2020, month)+h1Row(time.Date(2020, time.Month(month), 1, 0, 0, 0, 0, time.UTC), true))
	}
}

func TestInspect_GapSortTiebreaksAcrossIntervalAndYear(t *testing.T) {
	root := t.TempDir()
	buildTieBreakArchive(t, root)

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	type gapKey struct {
		interval RawInterval
		year     int
		month    time.Month
	}
	got := make(map[gapKey]bool, len(inv.Gaps))
	for _, g := range inv.Gaps {
		assert.Equal(t, "EURUSD", g.Symbol)
		got[gapKey{g.Interval, g.Year, g.Month}] = true
	}
	assert.True(t, got[gapKey{RawH1, 2020, time.June}])
	assert.True(t, got[gapKey{RawH1, 2021, time.February}], "same symbol+interval, different year")
	assert.True(t, got[gapKey{RawH4, 2020, time.June}], "same symbol, different interval")
	// H1 spans 2020-05 through 2021-03 (11 months) with 4 present -> 7
	// gaps; H4 spans 2020-05 through 2020-07 (3 months) with 2 present
	// -> 1 gap.
	assert.Len(t, inv.Gaps, 8)

	// Gaps must be sorted: Symbol, then Interval, then Year, then Month.
	for i := 1; i < len(inv.Gaps); i++ {
		a, b := inv.Gaps[i-1], inv.Gaps[i]
		key := func(g MonthGap) string {
			return fmt.Sprintf("%s|%s|%04d|%02d", g.Symbol, g.Interval, g.Year, int(g.Month))
		}
		assert.Less(t, key(a), key(b))
	}
}

func gbpusdID() instrument.ID {
	return instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
}

// findGaps is exercised directly (not through Inspect/filesystem walk) to
// reach two situations a directory walk's naturally sorted traversal
// order never produces: partitions arriving out of chronological order
// within one (instrument, interval) group, and two groups with
// different Symbols that must be compared during the final sort.
func TestFindGaps_OutOfOrderInputAndSymbolTiebreak(t *testing.T) {
	partitions := []Partition{
		{Instrument: eurusdID(), Symbol: "EURUSD", Interval: RawH1, Year: 2020, Month: time.August},
		{Instrument: eurusdID(), Symbol: "EURUSD", Interval: RawH1, Year: 2020, Month: time.May}, // out of order
		{Instrument: gbpusdID(), Symbol: "GBPUSD", Interval: RawH1, Year: 2020, Month: time.May},
		{Instrument: gbpusdID(), Symbol: "GBPUSD", Interval: RawH1, Year: 2020, Month: time.July},
	}

	gaps := findGaps(partitions)

	var symbols []string
	for _, g := range gaps {
		symbols = append(symbols, g.Symbol)
	}
	// EURUSD (May..August): gaps June, July. GBPUSD (May..July): gap June.
	assert.Equal(t, []string{"EURUSD", "EURUSD", "GBPUSD"}, symbols, "sorted by Symbol first")
	assert.Equal(t, time.June, gaps[0].Month)
	assert.Equal(t, time.July, gaps[1].Month)
	assert.Equal(t, time.June, gaps[2].Month)
}

// DuplicateTimes' sort comparator needs at least two duplicate groups to
// actually compare two elements.
func TestInspect_MultipleDuplicateTimesSorted(t *testing.T) {
	root := t.TempDir()
	dir := root + "/EURUSD/2020/05"
	writeFile(t, dir, "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+
			h1Row(time.Date(2020, 5, 1, 3, 0, 0, 0, time.UTC), true)+
			h1Row(time.Date(2020, 5, 1, 3, 0, 0, 0, time.UTC), true)+ // duplicate #1 (later time)
			h1Row(time.Date(2020, 5, 1, 1, 0, 0, 0, time.UTC), true)+
			h1Row(time.Date(2020, 5, 1, 1, 0, 0, 0, time.UTC), true)) // duplicate #2 (earlier time)

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	p := findPartition(t, inv, "EURUSD", RawH1, 2020, time.May)
	require.Len(t, p.DuplicateTimes, 2)
	assert.True(t, p.DuplicateTimes[0].Before(p.DuplicateTimes[1]), "sorted ascending")
	assert.Equal(t, time.Date(2020, 5, 1, 1, 0, 0, 0, time.UTC), p.DuplicateTimes[0])
	assert.Equal(t, time.Date(2020, 5, 1, 3, 0, 0, 0, time.UTC), p.DuplicateTimes[1])
}

// --- Path-layout verification (review finding) ---

// A file nested inside an unrelated subtree beneath root -- for example
// a "backup" directory sitting alongside the real pair directories --
// must not be accepted as an authoritative partition merely because its
// own file name happens to parse.
func TestInspect_NestedBackupTreeSkipped(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	nested := root + "/backup/EURUSD/2020/05"
	writeFile(t, nested, "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+h1Row(time.Date(2020, 5, 1, 9, 0, 0, 0, time.UTC), true))

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	for _, p := range inv.Partitions {
		if p.Symbol == "EURUSD" && p.Interval == RawH1 && p.Year == 2020 && p.Month == time.May {
			assert.NotContains(t, p.Path, "backup", "the nested backup copy must not be treated as the authoritative partition")
		}
	}

	var found *SkippedEntry
	for i, s := range inv.Skipped {
		if s.Path == filepath.Join(nested, "EURUSD-2020-05-h1.csv") {
			found = &inv.Skipped[i]
		}
	}
	require.NotNil(t, found, "the nested backup file must be reported as skipped")
	assert.ErrorIs(t, found.Reason, ErrMalformedData)
}

// A file whose own name parses to one partition but sits under a
// directory naming a *different* pair/year/month must not be silently
// accepted under the file name's identity: the directory placement
// disagreeing with the file name is itself a corruption signal.
func TestInspect_MisplacedFileSkipped(t *testing.T) {
	root := t.TempDir()
	buildArchive(t, root)

	// Filed under USDJPY/2021/06, but named as an EURUSD 2020-05 file.
	misplacedDir := root + "/USDJPY/2021/06"
	writeFile(t, misplacedDir, "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+h1Row(time.Date(2020, 5, 1, 9, 0, 0, 0, time.UTC), true))

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	for _, p := range inv.Partitions {
		if p.Symbol == "USDJPY" && p.Year == 2021 && p.Month == time.June {
			t.Fatalf("misplaced file must not become a USDJPY 2021-06 partition: %+v", p)
		}
	}

	var found *SkippedEntry
	for i, s := range inv.Skipped {
		if s.Path == filepath.Join(misplacedDir, "EURUSD-2020-05-h1.csv") {
			found = &inv.Skipped[i]
		}
	}
	require.NotNil(t, found, "the misplaced file must be reported as skipped")
	assert.ErrorIs(t, found.Reason, ErrMalformedData)
}

func TestInspect_WrongDepthBeneathRootSkipped(t *testing.T) {
	root := t.TempDir()
	// Only two path components beneath root, not the required three.
	writeFile(t, root+"/EURUSD", "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+h1Row(time.Date(2020, 5, 1, 9, 0, 0, 0, time.UTC), true))

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	assert.Empty(t, inv.Partitions)
	require.Len(t, inv.Skipped, 1)
	assert.ErrorIs(t, inv.Skipped[0].Reason, ErrMalformedData)
}

func TestInspect_DirectoryYearMismatchSkipped(t *testing.T) {
	root := t.TempDir()
	dir := root + "/EURUSD/2021/05" // directory year 2021, file name year 2020
	writeFile(t, dir, "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+h1Row(time.Date(2020, 5, 1, 9, 0, 0, 0, time.UTC), true))

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	assert.Empty(t, inv.Partitions)
	require.Len(t, inv.Skipped, 1)
	assert.ErrorIs(t, inv.Skipped[0].Reason, ErrMalformedData)
}

func TestInspect_DirectoryMonthMismatchSkipped(t *testing.T) {
	root := t.TempDir()
	dir := root + "/EURUSD/2020/06" // directory month 06, file name month 05
	writeFile(t, dir, "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+h1Row(time.Date(2020, 5, 1, 9, 0, 0, 0, time.UTC), true))

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	assert.Empty(t, inv.Partitions)
	require.Len(t, inv.Skipped, 1)
	assert.ErrorIs(t, inv.Skipped[0].Reason, ErrMalformedData)
}

// A valid header followed by a data row that fails to parse must still
// classify as Malformed, exercised through the shared in-memory
// (newReaderFromBytes-backed) parse path rather than a bad header.
func TestInspect_MalformedDataRowAfterValidHeader(t *testing.T) {
	root := t.TempDir()
	dir := root + "/EURUSD/2020/05"
	writeFile(t, dir, "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+"not-a-valid-row-at-all\n")

	inv, err := Inspect(context.Background(), root)
	require.NoError(t, err)

	p := findPartition(t, inv, "EURUSD", RawH1, 2020, time.May)
	assert.Equal(t, PartitionStatusMalformed, p.Status)
	assert.ErrorIs(t, p.Err, ErrMalformedData)
}

// --- Context cancellation during file parsing (review finding) ---

// A context cancelled before inspectFile parses a file's rows must
// propagate as cancellation, not be misclassified as
// PartitionStatusMalformed. This exercises the same failure mode
// Inspect's own per-file WalkDir pre-check cannot catch: cancellation
// observed by Reader.Next from *inside* inspectFile's own work, not
// between files.
func TestInspectFile_ContextCancelledDuringParseIsFatal(t *testing.T) {
	root := t.TempDir()
	dir := root + "/EURUSD/2020/05"
	writeFile(t, dir, "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+h1Row(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), true))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p, skipped, fatalErr := inspectFile(ctx, root, filepath.Join(dir, "EURUSD-2020-05-h1.csv"))
	require.ErrorIs(t, fatalErr, context.Canceled)
	assert.Zero(t, p, "no partition result when inspectFile fails fatally")
	assert.Nil(t, skipped, "cancellation is not a skip reason")
}

// Companion to TestInspectFile_ContextCancelledDuringParseIsFatal, at
// the Inspect level: proves the fatal error inspectFile now returns
// actually propagates all the way out through WalkDir and Inspect's own
// error wrapping as context.Canceled, and that Inspect never reports a
// successful Inventory containing a bogus Malformed partition when that
// happens.
func TestInspect_ContextCancelledDuringParsePropagates(t *testing.T) {
	root := t.TempDir()
	dir := root + "/EURUSD/2020/05"
	writeFile(t, dir, "EURUSD-2020-05-h1.csv",
		fmtHeader("EURUSD", 2020, 5)+h1Row(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), true))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	inv, err := Inspect(ctx, root)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, inv.Partitions)
	assert.Empty(t, inv.Skipped)
}
