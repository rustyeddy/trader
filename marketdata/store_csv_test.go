package marketdata

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validPartitionKey matches validManifest/validBarSet's EUR/USD H1
// span (2020-03-02 00:00 through 04:00 UTC).
func validPartitionKey(t *testing.T) partitionKey {
	t.Helper()
	return partitionKey{
		provider:   "oanda",
		symbol:     "EURUSD",
		instrument: eurusd(),
		interval:   H1,
		year:       2020,
		month:      time.March,
	}
}

func instrumentUSDJPY() instrument.ID {
	return instrument.CurrencyPairID(num.MustParseCurrency("USD"), num.MustParseCurrency("JPY"))
}

// --- Reusable contract tests (issue #77): run against any barStore ---
//
// A later store implementation (a Parquet store, for example) can reuse
// this exact function against its own constructor.

func testBarStoreContract(t *testing.T, newStore func(root string) barStore) {
	t.Helper()

	t.Run("PublishThenLoadRoundTrips", func(t *testing.T) {
		store := newStore(t.TempDir())
		key := validPartitionKey(t)
		m := validManifest(t)
		bs := validBarSet(t)
		require.NoError(t, m.Matches(bs))

		require.NoError(t, store.publish(context.Background(), key, m, bs))

		gotM, gotBS, err := store.load(context.Background(), key)
		require.NoError(t, err)
		assert.Equal(t, m.Revision(), gotM.Revision())
		require.Len(t, gotBS.Bars, len(bs.Bars))
		for i := range bs.Bars {
			assert.Equal(t, bs.Bars[i], gotBS.Bars[i], "bar %d", i)
		}
	})

	t.Run("OrderingPreserved", func(t *testing.T) {
		store := newStore(t.TempDir())
		key := validPartitionKey(t)
		bs := validBarSet(t)
		m := validManifest(t)
		require.NoError(t, store.publish(context.Background(), key, m, bs))

		_, gotBS, err := store.load(context.Background(), key)
		require.NoError(t, err)
		for i := 1; i < len(gotBS.Bars); i++ {
			assert.True(t, gotBS.Bars[i].Time.After(gotBS.Bars[i-1].Time), "bars must stay strictly ordered")
		}
	})

	t.Run("EmptyBarSetRoundTrips", func(t *testing.T) {
		store := newStore(t.TempDir())
		key := validPartitionKey(t)
		m := validManifest(t)
		m.BarCount = 0
		m.FirstBar = time.Time{}
		m.LastBar = time.Time{}
		bs := validBarSet(t)
		bs.Bars = nil
		require.NoError(t, m.Matches(bs))

		require.NoError(t, store.publish(context.Background(), key, m, bs))
		_, gotBS, err := store.load(context.Background(), key)
		require.NoError(t, err)
		assert.Empty(t, gotBS.Bars, "no dummy bars for an empty published set")
	})

	t.Run("LoadMissingPartitionErrors", func(t *testing.T) {
		store := newStore(t.TempDir())
		_, _, err := store.load(context.Background(), validPartitionKey(t))
		assert.Error(t, err)
	})

	t.Run("PublishRejectsMismatchedPair", func(t *testing.T) {
		store := newStore(t.TempDir())
		key := validPartitionKey(t)
		m := validManifest(t)
		bs := validBarSet(t)
		bs.Bars = bs.Bars[:1] // BarCount now disagrees with m
		err := store.publish(context.Background(), key, m, bs)
		assert.Error(t, err)
	})

	t.Run("PublishRejectsInvalidManifest", func(t *testing.T) {
		store := newStore(t.TempDir())
		key := validPartitionKey(t)
		m := validManifest(t)
		m.Provider = "" // invalid, and also now disagrees with key.provider
		bs := validBarSet(t)
		err := store.publish(context.Background(), key, m, bs)
		assert.Error(t, err)
	})

	t.Run("PublishCancelledBeforeWriteLeavesPriorRevisionIntact", func(t *testing.T) {
		store := newStore(t.TempDir())
		key := validPartitionKey(t)
		m := validManifest(t)
		bs := validBarSet(t)
		require.NoError(t, store.publish(context.Background(), key, m, bs))

		// A second publish, cancelled before it can do anything.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		m2 := validManifest(t)
		m2.BuilderVersion = "builder-v2" // would change Revision if published
		err := store.publish(ctx, key, m2, bs)
		assert.ErrorIs(t, err, context.Canceled)

		gotM, _, err := store.load(context.Background(), key)
		require.NoError(t, err)
		assert.Equal(t, m.Revision(), gotM.Revision(), "the original revision must still be the one published")
	})

	t.Run("RepublishReplacesThePriorRevision", func(t *testing.T) {
		store := newStore(t.TempDir())
		key := validPartitionKey(t)
		m := validManifest(t)
		bs := validBarSet(t)
		require.NoError(t, store.publish(context.Background(), key, m, bs))

		m2 := validManifest(t)
		m2.BuilderVersion = "builder-v2"
		require.NoError(t, store.publish(context.Background(), key, m2, bs))

		gotM, _, err := store.load(context.Background(), key)
		require.NoError(t, err)
		assert.Equal(t, m2.Revision(), gotM.Revision())
		assert.NotEqual(t, m.Revision(), gotM.Revision())
	})
}

func TestCanonicalCSVStore_Contract(t *testing.T) {
	testBarStoreContract(t, func(root string) barStore {
		return newCanonicalCSVStore(root)
	})
}

// --- CSV-specific tests ---

func TestCanonicalCSVStore_Root(t *testing.T) {
	s := newCanonicalCSVStore("/data/candles")
	assert.Equal(t, "/data/candles", s.root())
}

func TestPartitionKey_PathConvention(t *testing.T) {
	key := validPartitionKey(t)
	path, err := key.path("/root")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/root", "oanda", "EURUSD", "2020", "03", "EURUSD-2020-03-h1.csv"), path)
}

func TestIntervalToken(t *testing.T) {
	cases := map[Interval]string{M1: "m1", H1: "h1", H4: "h4", D1: "d1", W1: "w1"}
	for interval, want := range cases {
		got, err := intervalToken(interval)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestIntervalToken_Unsupported(t *testing.T) {
	weird, err := NewInterval(UnitMinute, 7)
	require.NoError(t, err)
	_, err = intervalToken(weird)
	assert.ErrorIs(t, err, errStoreUnsupportedInterval)
}

func TestCanonicalCSVStore_ExactDecimalPrecision(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := partitionKey{provider: "oanda", symbol: "USDJPY", instrument: instrumentUSDJPY(), interval: H1, year: 2020, month: time.May}

	start := time.Date(2020, time.May, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, time.May, 1, 1, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(start, end)
	require.NoError(t, err)
	bar := Bar{
		Time:      start,
		Open:      p(t, "107.27200"),
		High:      p(t, "107.30000"),
		Low:       p(t, "107.20000"),
		Close:     p(t, "107.25000"),
		AvgSpread: p(t, "0.011"),
		MaxSpread: p(t, "0.011"),
		Ticks:     500,
	}
	bs := BarSet{Instrument: key.instrument, Interval: H1, Span: span, Basis: BasisBid, Bars: []Bar{bar}}
	m := validManifest(t)
	m.Instrument = key.instrument
	m.Span = span
	m.BarCount = 1
	m.FirstBar = start
	m.LastBar = start
	require.NoError(t, m.Matches(bs))

	require.NoError(t, store.publish(context.Background(), key, m, bs))
	_, gotBS, err := store.load(context.Background(), key)
	require.NoError(t, err)
	require.Len(t, gotBS.Bars, 1)
	assert.Equal(t, bar, gotBS.Bars[0], "exact decimal round trip, never through float64")
}

func TestCanonicalCSVStore_SchemaMismatchRejected(t *testing.T) {
	root := t.TempDir()
	store := newCanonicalCSVStore(root)
	key := validPartitionKey(t)
	require.NoError(t, store.publish(context.Background(), key, validManifest(t), validBarSet(t)))

	path, err := key.path(root)
	require.NoError(t, err)
	corrupt := "# schema=canonical-v1 provider=oanda symbol=GBPUSD interval=h1 year=2020 month=03\n" +
		"{}\n" + canonicalCSVHeader + "\n"
	require.NoError(t, os.WriteFile(path, []byte(corrupt), 0o644))

	_, _, err = store.load(context.Background(), key)
	assert.ErrorIs(t, err, errStoreMalformed)
}

func TestCanonicalCSVStore_MalformedRowRejected(t *testing.T) {
	root := t.TempDir()
	store := newCanonicalCSVStore(root)
	key := validPartitionKey(t)
	require.NoError(t, store.publish(context.Background(), key, validManifest(t), validBarSet(t)))

	path, err := key.path(root)
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, []byte("not,a,valid,row\n")...), 0o644))

	_, _, err = store.load(context.Background(), key)
	assert.ErrorIs(t, err, errStoreMalformed)
}

func TestCanonicalCSVStore_ManifestInstrumentMismatchRejected(t *testing.T) {
	root := t.TempDir()
	store := newCanonicalCSVStore(root)
	key := validPartitionKey(t)
	require.NoError(t, store.publish(context.Background(), key, validManifest(t), validBarSet(t)))

	// Loading with a key naming a different instrument must fail: the
	// stored header's instrument field disagrees with what's expected.
	wrongKey := key
	wrongKey.instrument = instrumentUSDJPY()
	_, _, err := store.load(context.Background(), wrongKey)
	assert.Error(t, err)
}

func TestCanonicalCSVStore_ManifestWithParentRoundTrips(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	m := validManifest(t)
	m.ResamplerVersion = "resampler-v1"
	m.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "parent-rev-1"}
	bs := validBarSet(t)
	require.NoError(t, m.Matches(bs))

	require.NoError(t, store.publish(context.Background(), key, m, bs))
	gotM, _, err := store.load(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, gotM.Parent)
	assert.Equal(t, m.Parent.Instrument, gotM.Parent.Instrument)
	assert.Equal(t, m.Parent.Interval, gotM.Parent.Interval)
	assert.Equal(t, m.Parent.Revision, gotM.Parent.Revision)
	assert.Equal(t, m.Revision(), gotM.Revision())
}

func TestCanonicalCSVStore_PublishLeavesNoTempFilesBehind(t *testing.T) {
	root := t.TempDir()
	store := newCanonicalCSVStore(root)
	key := validPartitionKey(t)
	require.NoError(t, store.publish(context.Background(), key, validManifest(t), validBarSet(t)))

	dir := key.dir(root)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp-", "no leftover temp file after a successful publish")
	}
}

func TestWriteFileAtomic_FailureLeavesNoFinalFile(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "out.csv")
	boom := errors.New("boom")

	err := writeFileAtomic(context.Background(), final, func(w *bufio.Writer) error {
		return boom
	})
	assert.ErrorIs(t, err, boom)
	_, statErr := os.Stat(final)
	assert.True(t, os.IsNotExist(statErr))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the failed temp file must be cleaned up")
}

// --- Path safety (review finding) ---

func TestValidatePathComponent(t *testing.T) {
	valid := []string{"oanda", "EURUSD", "a.b", "a-b_c"}
	for _, v := range valid {
		assert.NoError(t, validatePathComponent(v), "%q should be valid", v)
	}

	invalid := []string{"", ".", "..", "a/b", "../escape", "a/../b"}
	for _, v := range invalid {
		assert.Error(t, validatePathComponent(v), "%q should be rejected", v)
	}
}

func TestPartitionKey_ValidateRejectsTraversal(t *testing.T) {
	cases := []partitionKey{
		{provider: "../escape", symbol: "EURUSD", instrument: eurusd(), interval: H1, year: 2020, month: time.May},
		{provider: "oanda", symbol: "../../escape", instrument: eurusd(), interval: H1, year: 2020, month: time.May},
		{provider: "oanda/sub", symbol: "EURUSD", instrument: eurusd(), interval: H1, year: 2020, month: time.May},
	}
	for _, key := range cases {
		assert.ErrorIs(t, key.validate(), errStoreInvalidPartitionKey)
	}
}

func TestCanonicalCSVStore_PublishRejectsPathTraversalKey(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	key.symbol = "../../escape"
	m := validManifest(t)
	m.Provider = key.provider
	err := store.publish(context.Background(), key, m, validBarSet(t))
	assert.ErrorIs(t, err, errStoreInvalidPartitionKey)
}

// --- Key/manifest agreement (review finding) ---

func TestCanonicalCSVStore_PublishRejectsKeyInstrumentMismatch(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	key.instrument = instrumentUSDJPY() // disagrees with validManifest's EUR/USD
	err := store.publish(context.Background(), key, validManifest(t), validBarSet(t))
	assert.ErrorIs(t, err, errStorePartitionKeyMismatch)
}

func TestCanonicalCSVStore_PublishRejectsKeyProviderMismatch(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	key.provider = "alpaca"
	err := store.publish(context.Background(), key, validManifest(t), validBarSet(t))
	assert.ErrorIs(t, err, errStorePartitionKeyMismatch)
}

func TestCanonicalCSVStore_PublishRejectsKeyIntervalMismatch(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	key.interval = D1 // validManifest/validBarSet use H1
	err := store.publish(context.Background(), key, validManifest(t), validBarSet(t))
	assert.ErrorIs(t, err, errStorePartitionKeyMismatch)
}

func TestCanonicalCSVStore_PublishRejectsKeyMonthMismatch(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	key.month = time.December // validManifest/validBarSet's span is entirely in May
	err := store.publish(context.Background(), key, validManifest(t), validBarSet(t))
	assert.ErrorIs(t, err, errStorePartitionKeyMismatch)
}

func TestCanonicalCSVStore_PublishAcceptsMonthBoundaryOverlap(t *testing.T) {
	// A span that starts in the last hour of April but is filed under
	// May must be accepted: checkKeyMatchesManifest checks overlap, not
	// containment.
	store := newCanonicalCSVStore(t.TempDir())
	start := time.Date(2020, time.April, 30, 23, 0, 0, 0, time.UTC)
	end := time.Date(2020, time.May, 1, 1, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(start, end)
	require.NoError(t, err)

	key := validPartitionKey(t)
	key.month = time.May
	m := validManifest(t)
	m.Span = span
	m.BarCount = 0
	m.FirstBar = time.Time{}
	m.LastBar = time.Time{}
	bs := validBarSet(t)
	bs.Span = span
	bs.Bars = nil
	require.NoError(t, m.Matches(bs))

	assert.NoError(t, store.publish(context.Background(), key, m, bs))
}

// --- Atomicity: single-file publish (review finding) ---

// nthCancelContext reports context.Canceled from Err() once its
// remaining count is exhausted, letting a test control precisely which
// of publish's ctx.Err() checks first observes cancellation.
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

// Regression test for the design-review fix: cancellation landing after
// key/manifest validation but before the (now single) file write must
// leave the prior revision not merely detectably-inconsistent, but
// fully loadable.
func TestCanonicalCSVStore_PublishCancelledJustBeforeWriteLoadsOriginalRevision(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	m := validManifest(t)
	bs := validBarSet(t)
	require.NoError(t, store.publish(context.Background(), key, m, bs))

	m2 := validManifest(t)
	m2.BuilderVersion = "builder-v2"
	// publish checks ctx.Err() twice before ever calling writeFileAtomic
	// (top of publish, then again just before it); allow both to pass,
	// then cancel right as writeFileAtomic/encodePartition would begin.
	ctx := &nthCancelContext{Context: context.Background(), remaining: 1}
	err := store.publish(ctx, key, m2, bs)
	assert.ErrorIs(t, err, context.Canceled)

	gotM, gotBS, err := store.load(context.Background(), key)
	require.NoError(t, err, "the original revision must still load successfully, not merely fail Matches")
	assert.Equal(t, m.Revision(), gotM.Revision())
	assert.Equal(t, len(bs.Bars), len(gotBS.Bars))
}

// Regression test for the second review finding: publish's earlier
// ctx.Err() checks only ran before any writing began, so cancellation
// arriving while encodePartition's bar loop was still running -- after
// real work had already started -- went unnoticed until the write
// completed successfully. encodePartition now checks ctx.Err() on every
// bar; this confirms cancellation landing after the first of
// validBarSet's two bars, but before the second, is caught before
// anything is renamed into place.
func TestCanonicalCSVStore_PublishCancelledDuringEncodeLoadsOriginalRevision(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	m := validManifest(t)
	bs := validBarSet(t)
	require.GreaterOrEqual(t, len(bs.Bars), 2, "fixture must have at least two bars for this test to be meaningful")
	require.NoError(t, store.publish(context.Background(), key, m, bs))

	m2 := validManifest(t)
	m2.BuilderVersion = "builder-v2"
	// Checks, in order: top of publish, pre-write, bar[0]'s loop check
	// (all must pass) -- then bar[1]'s loop check must observe
	// cancellation.
	ctx := &nthCancelContext{Context: context.Background(), remaining: 3}
	err := store.publish(ctx, key, m2, bs)
	assert.ErrorIs(t, err, context.Canceled)

	gotM, gotBS, err := store.load(context.Background(), key)
	require.NoError(t, err, "the original revision must still load successfully after mid-encode cancellation")
	assert.Equal(t, m.Revision(), gotM.Revision())
	assert.Equal(t, len(bs.Bars), len(gotBS.Bars))

	dir := key.dir(store.root())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp-", "the abandoned temp file must not be left behind")
	}
}

// Regression test for the commit-point check itself: cancellation
// landing after encoding, flushing, and syncing have all already
// succeeded -- with nothing left but the rename -- must still be caught
// before that rename, per writeFileAtomic's own commit-point check.
func TestCanonicalCSVStore_PublishCancelledJustBeforeRenameLoadsOriginalRevision(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	m := validManifest(t)
	bs := validBarSet(t)
	require.NoError(t, store.publish(context.Background(), key, m, bs))

	m2 := validManifest(t)
	m2.BuilderVersion = "builder-v2"
	// Checks, in order: top of publish, pre-write, one per bar (two
	// bars in the fixture) -- all five must pass -- then the
	// commit-point check in writeFileAtomic, immediately before
	// os.Rename, must observe cancellation.
	ctx := &nthCancelContext{Context: context.Background(), remaining: 4}
	err := store.publish(ctx, key, m2, bs)
	assert.ErrorIs(t, err, context.Canceled)

	gotM, _, err := store.load(context.Background(), key)
	require.NoError(t, err, "the original revision must still load successfully after cancellation right before commit")
	assert.Equal(t, m.Revision(), gotM.Revision())
}

// --- checkKeyMatchesManifest direct tests ---

func TestCheckKeyMatchesManifest_OK(t *testing.T) {
	assert.NoError(t, checkKeyMatchesManifest(validPartitionKey(t), validManifest(t)))
}

// --- decodeManifestJSON / encodePartition direct tests ---

func TestDecodeManifestJSON_RevisionMismatchRejected(t *testing.T) {
	m := validManifest(t)
	h := manifestToJSON(m)
	h.Revision = "tampered"
	encoded, err := json.Marshal(h)
	require.NoError(t, err)

	_, err = decodeManifestJSON(string(encoded), "path", m.Instrument)
	assert.ErrorIs(t, err, errStoreMalformed)
}

func TestDecodeManifestJSON_MalformedJSON(t *testing.T) {
	_, err := decodeManifestJSON("not json", "path", eurusd())
	assert.ErrorIs(t, err, errStoreMalformed)
}

func TestDecodeManifestJSON_ParentInstrumentMismatchRejected(t *testing.T) {
	m := validManifest(t)
	m.ResamplerVersion = "resampler-v1"
	m.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "parent-rev-1"}
	h := manifestToJSON(m)
	h.Parent.Instrument = instrumentUSDJPY().String()
	// Re-derive Revision so only the parent-instrument cross-check (not
	// the revision check) is what rejects this.
	h.Revision = m.Revision()
	encoded, err := json.Marshal(h)
	require.NoError(t, err)

	_, err = decodeManifestJSON(string(encoded), "path", m.Instrument)
	assert.ErrorIs(t, err, errStoreMalformed)
}

func TestDecodeManifestJSON_EmbeddedNewlinesRoundTrip(t *testing.T) {
	// A line-oriented key=value encoding could not survive this; JSON
	// must.
	m := validManifest(t)
	m.BuilderVersion = "builder-v1\nrevision=forged\n"
	m.ValidatorVersion = "validator\rwith-cr"

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	require.NoError(t, encodePartition(context.Background(), w, validPartitionKey(t), m, validBarSet(t)))
	require.NoError(t, w.Flush())

	scanner := bufio.NewScanner(&buf)
	require.True(t, scanner.Scan()) // schema comment
	require.True(t, scanner.Scan()) // manifest header
	got, err := decodeManifestJSON(scanner.Text(), "path", m.Instrument)
	require.NoError(t, err)
	assert.Equal(t, m.BuilderVersion, got.BuilderVersion)
	assert.Equal(t, m.ValidatorVersion, got.ValidatorVersion)
}

// --- Additional coverage ---

func TestPartitionKey_ValidateRejectsZeroInstrumentAndInvalidInterval(t *testing.T) {
	key := validPartitionKey(t)
	key.instrument = instrument.ID{}
	assert.ErrorIs(t, key.validate(), errStoreInvalidPartitionKey)

	key2 := validPartitionKey(t)
	key2.interval = Interval{}
	assert.ErrorIs(t, key2.validate(), errStoreInvalidPartitionKey)

	key3 := validPartitionKey(t)
	key3.month = 0
	assert.ErrorIs(t, key3.validate(), errStoreInvalidPartitionKey)
}

func TestCanonicalCSVStore_LoadRejectsInvalidKey(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	key.instrument = instrument.ID{}
	_, _, err := store.load(context.Background(), key)
	assert.ErrorIs(t, err, errStoreInvalidPartitionKey)
}

func TestParseBarRow_MalformedCases(t *testing.T) {
	valid := "2020-03-02T00:00:00Z,1.10000,1.10250,1.09900,1.10100,0.00012,0.00030,4213"
	_, err := parseBarRow(valid)
	require.NoError(t, err, "sanity: the valid fixture itself must parse")

	cases := map[string]string{
		"wrong field count": "2020-03-02T00:00:00Z,1.1,1.1,1.1,1.1",
		"bad time":          "not-a-time,1.10000,1.10250,1.09900,1.10100,0.00012,0.00030,4213",
		"bad open price":    "2020-03-02T00:00:00Z,not-a-price,1.10250,1.09900,1.10100,0.00012,0.00030,4213",
		"bad ticks":         "2020-03-02T00:00:00Z,1.10000,1.10250,1.09900,1.10100,0.00012,0.00030,not-an-int",
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseBarRow(row)
			assert.Error(t, err)
		})
	}
}

func TestCrossCheckPartitionSchema_FieldMismatches(t *testing.T) {
	key := validPartitionKey(t)
	cases := map[string]string{
		"schema":   "# schema=wrong-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=03",
		"provider": "# schema=canonical-v1 provider=WRONG symbol=EURUSD interval=h1 year=2020 month=03",
		"symbol":   "# schema=canonical-v1 provider=oanda symbol=WRONG interval=h1 year=2020 month=03",
		"interval": "# schema=canonical-v1 provider=oanda symbol=EURUSD interval=d1 year=2020 month=03",
		"year":     "# schema=canonical-v1 provider=oanda symbol=EURUSD interval=h1 year=1999 month=03",
		"month":    "# schema=canonical-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=01",
	}
	for name, comment := range cases {
		t.Run(name, func(t *testing.T) {
			err := crossCheckPartitionSchema(comment, "path", key)
			assert.ErrorIs(t, err, errStoreMalformed)
		})
	}
}

func TestReadPartitionFile_EmptyMissingHeaderAndManifest(t *testing.T) {
	root := t.TempDir()
	key := validPartitionKey(t)
	dir := key.dir(root)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path, err := key.path(root)
	require.NoError(t, err)

	t.Run("empty file", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, []byte(""), 0o644))
		_, _, err := readPartitionFile(path, key)
		assert.ErrorIs(t, err, errStoreMalformed)
	})

	t.Run("missing manifest header", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path,
			[]byte("# schema=canonical-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=03\n"), 0o644))
		_, _, err := readPartitionFile(path, key)
		assert.ErrorIs(t, err, errStoreMalformed)
	})

	t.Run("missing column header", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path,
			[]byte("# schema=canonical-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=03\n{}\n"), 0o644))
		_, _, err := readPartitionFile(path, key)
		assert.ErrorIs(t, err, errStoreMalformed)
	})

	t.Run("wrong column header", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path,
			[]byte("# schema=canonical-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=03\n{}\nwrong,header\n"), 0o644))
		_, _, err := readPartitionFile(path, key)
		assert.ErrorIs(t, err, errStoreMalformed)
	})
}

func TestDecodeManifestJSON_MalformedFields(t *testing.T) {
	m := validManifest(t)
	base := manifestToJSON(m)

	mutate := map[string]func(*manifestJSON){
		"interval_unit": func(h *manifestJSON) { h.IntervalUnit = Unit(200) },
		"span_start":    func(h *manifestJSON) { h.SpanStart = "not-a-time" },
		"span_end":      func(h *manifestJSON) { h.SpanEnd = "not-a-time" },
		"built_at":      func(h *manifestJSON) { h.BuiltAt = "not-a-time" },
		"first_bar":     func(h *manifestJSON) { h.FirstBar = "not-a-time" },
		"last_bar":      func(h *manifestJSON) { h.LastBar = "not-a-time" },
		"span reversed": func(h *manifestJSON) { h.SpanStart, h.SpanEnd = h.SpanEnd, h.SpanStart },
		"instrument":    func(h *manifestJSON) { h.Instrument = "wrong" },
		"parent interval": func(h *manifestJSON) {
			h.Parent = &parentJSON{Instrument: m.Instrument.String(), IntervalUnit: Unit(200), IntervalCount: 1, Revision: "r"}
		},
	}
	for name, fn := range mutate {
		t.Run(name, func(t *testing.T) {
			h := base
			fn(&h)
			// Recompute revision is deliberately skipped: these are all
			// cases that must fail before, or independently of, the
			// revision cross-check.
			encoded, err := json.Marshal(h)
			require.NoError(t, err)
			_, err = decodeManifestJSON(string(encoded), "path", m.Instrument)
			assert.Error(t, err)
		})
	}
}
