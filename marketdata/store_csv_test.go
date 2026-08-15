package marketdata

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validPartitionKey matches validManifest/validBarSet's EUR/USD H1
// span (2020-05, 2020-05-02 00:00 through 04:00 UTC).
func validPartitionKey(t *testing.T) partitionKey {
	t.Helper()
	return partitionKey{
		provider:   "oanda",
		symbol:     "EURUSD",
		instrument: eurusd(),
		interval:   H1,
		year:       2020,
		month:      time.May,
	}
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
		m.Provider = "" // invalid
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
	dataPath, err := key.dataPath("/root")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/root", "oanda", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.csv"), dataPath)

	manifestPath, err := key.manifestPath("/root")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/root", "oanda", "EURUSD", "2020", "05", "EURUSD-2020-05-h1.manifest"), manifestPath)
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

func TestCanonicalCSVStore_BarSchemaMismatchRejected(t *testing.T) {
	root := t.TempDir()
	store := newCanonicalCSVStore(root)
	key := validPartitionKey(t)
	require.NoError(t, store.publish(context.Background(), key, validManifest(t), validBarSet(t)))

	dataPath, err := key.dataPath(root)
	require.NoError(t, err)
	corrupt := "# schema=canonical-bar-v1 provider=oanda symbol=GBPUSD interval=h1 year=2020 month=05\n" +
		canonicalCSVHeader + "\n"
	require.NoError(t, os.WriteFile(dataPath, []byte(corrupt), 0o644))

	_, _, err = store.load(context.Background(), key)
	assert.ErrorIs(t, err, errStoreMalformed)
}

func TestCanonicalCSVStore_MalformedRowRejected(t *testing.T) {
	root := t.TempDir()
	store := newCanonicalCSVStore(root)
	key := validPartitionKey(t)
	require.NoError(t, store.publish(context.Background(), key, validManifest(t), validBarSet(t)))

	dataPath, err := key.dataPath(root)
	require.NoError(t, err)
	corrupt := "# schema=canonical-bar-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=05\n" +
		canonicalCSVHeader + "\n" +
		"not,a,valid,row\n"
	require.NoError(t, os.WriteFile(dataPath, []byte(corrupt), 0o644))

	_, _, err = store.load(context.Background(), key)
	assert.ErrorIs(t, err, errStoreMalformed)
}

func TestCanonicalCSVStore_ManifestInstrumentMismatchRejected(t *testing.T) {
	root := t.TempDir()
	store := newCanonicalCSVStore(root)
	key := validPartitionKey(t)
	require.NoError(t, store.publish(context.Background(), key, validManifest(t), validBarSet(t)))

	manifestPath, err := key.manifestPath(root)
	require.NoError(t, err)
	_, err = readManifestFile(manifestPath, instrumentUSDJPY())
	assert.ErrorIs(t, err, errStoreMalformed)
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

	err := writeFileAtomic(final, func(w *bufio.Writer) error {
		return boom
	})
	assert.ErrorIs(t, err, boom)
	_, statErr := os.Stat(final)
	assert.True(t, os.IsNotExist(statErr))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the failed temp file must be cleaned up")
}

func instrumentUSDJPY() instrument.ID {
	return instrument.CurrencyPairID(num.MustParseCurrency("USD"), num.MustParseCurrency("JPY"))
}

// --- Additional coverage: path/error propagation and malformed input ---

func TestPartitionKey_DataPathPropagatesIntervalError(t *testing.T) {
	weird, err := NewInterval(UnitMinute, 7)
	require.NoError(t, err)
	key := partitionKey{provider: "oanda", symbol: "EURUSD", interval: weird, year: 2020, month: time.May}
	_, err = key.dataPath("/root")
	assert.ErrorIs(t, err, errStoreUnsupportedInterval)
	_, err = key.manifestPath("/root")
	assert.ErrorIs(t, err, errStoreUnsupportedInterval)
}

func TestCanonicalCSVStore_PublishRejectsInvalidBarSet(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	bs := validBarSet(t)
	// Break strict ordering: BarSet.Validate must reject this.
	bs.Bars[0], bs.Bars[1] = bs.Bars[1], bs.Bars[0]
	err := store.publish(context.Background(), key, validManifest(t), bs)
	assert.Error(t, err)
}

func TestEncodeBarSet_UnsupportedIntervalPropagates(t *testing.T) {
	weird, err := NewInterval(UnitMinute, 7)
	require.NoError(t, err)
	key := partitionKey{provider: "oanda", symbol: "EURUSD", interval: weird, year: 2020, month: time.May}
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	err = encodeBarSet(w, key, validBarSet(t))
	assert.ErrorIs(t, err, errStoreUnsupportedInterval)
}

func TestCrossCheckBarSchema_FieldMismatches(t *testing.T) {
	key := validPartitionKey(t)
	base := "# schema=canonical-bar-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=05"

	cases := map[string]string{
		"provider": "# schema=canonical-bar-v1 provider=WRONG symbol=EURUSD interval=h1 year=2020 month=05",
		"symbol":   "# schema=canonical-bar-v1 provider=oanda symbol=WRONG interval=h1 year=2020 month=05",
		"interval": "# schema=canonical-bar-v1 provider=oanda symbol=EURUSD interval=d1 year=2020 month=05",
		"year":     "# schema=canonical-bar-v1 provider=oanda symbol=EURUSD interval=h1 year=1999 month=05",
		"month":    "# schema=canonical-bar-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=01",
		"schema":   "# schema=wrong-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=05",
	}
	require.NotEqual(t, base, cases["provider"]) // sanity: fixtures actually differ

	for name, comment := range cases {
		t.Run(name, func(t *testing.T) {
			err := crossCheckBarSchema(comment, "path", key)
			assert.ErrorIs(t, err, errStoreMalformed)
		})
	}
}

func TestParseBarRow_MalformedCases(t *testing.T) {
	valid := "2020-05-01T00:00:00Z,1.10000,1.10250,1.09900,1.10100,0.00012,0.00030,4213"
	_, err := parseBarRow(valid)
	require.NoError(t, err, "sanity: the valid fixture itself must parse")

	cases := map[string]string{
		"wrong field count": "2020-05-01T00:00:00Z,1.1,1.1,1.1,1.1",
		"bad time":          "not-a-time,1.10000,1.10250,1.09900,1.10100,0.00012,0.00030,4213",
		"bad open price":    "2020-05-01T00:00:00Z,not-a-price,1.10250,1.09900,1.10100,0.00012,0.00030,4213",
		"bad ticks":         "2020-05-01T00:00:00Z,1.10000,1.10250,1.09900,1.10100,0.00012,0.00030,not-an-int",
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseBarRow(row)
			assert.Error(t, err)
		})
	}
}

func TestReadBarSetFile_EmptyAndMissingHeader(t *testing.T) {
	root := t.TempDir()
	key := validPartitionKey(t)
	dir := key.dir(root)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path, err := key.dataPath(root)
	require.NoError(t, err)

	t.Run("empty file", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, []byte(""), 0o644))
		_, err := readBarSetFile(path, key)
		assert.ErrorIs(t, err, errStoreMalformed)
	})

	t.Run("missing column header", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path,
			[]byte("# schema=canonical-bar-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=05\n"), 0o644))
		_, err := readBarSetFile(path, key)
		assert.ErrorIs(t, err, errStoreMalformed)
	})

	t.Run("wrong column header", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path,
			[]byte("# schema=canonical-bar-v1 provider=oanda symbol=EURUSD interval=h1 year=2020 month=05\nwrong,header\n"), 0o644))
		_, err := readBarSetFile(path, key)
		assert.ErrorIs(t, err, errStoreMalformed)
	})
}

// encodedManifestLines returns the exact lines encodeManifest produces
// for m, for tests that corrupt one field at a time.
func encodedManifestLines(t *testing.T, m Manifest) []string {
	t.Helper()
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	require.NoError(t, encodeManifest(w, m))
	require.NoError(t, w.Flush())
	lines := []string{}
	for l := range strings.SplitSeq(buf.String(), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestReadManifestFile_MalformedFields(t *testing.T) {
	m := validManifest(t)
	replacements := map[string]string{
		"interval_unit=":  "interval_unit=not-a-number\n",
		"interval_count=": "interval_count=not-a-number\n",
		"span_start=":     "span_start=not-a-time\n",
		"basis=":          "basis=not-a-number\n",
		"schema_version=": "schema_version=not-a-number\n",
		"built_at=":       "built_at=not-a-time\n",
		"bar_count=":      "bar_count=not-a-number\n",
		"span_end=":       "span_end=not-a-time\n",
		"first_bar=":      "first_bar=not-a-time\n",
		"last_bar=":       "last_bar=not-a-time\n",
	}

	for prefix, replacement := range replacements {
		t.Run(prefix, func(t *testing.T) {
			lines := encodedManifestLines(t, m)
			var out string
			for _, l := range lines {
				if strings.HasPrefix(l, prefix) {
					out += replacement
				} else {
					out += l + "\n"
				}
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "test.manifest")
			require.NoError(t, os.WriteFile(path, []byte(out), 0o644))
			_, err := readManifestFile(path, m.Instrument)
			assert.ErrorIs(t, err, errStoreMalformed)
		})
	}
}

func TestReadManifestFile_SpanEndBeforeStart(t *testing.T) {
	m := validManifest(t)
	lines := encodedManifestLines(t, m)
	var out string
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "span_start="):
			out += "span_start=" + m.Span.End().Format(time.RFC3339Nano) + "\n"
		case strings.HasPrefix(l, "span_end="):
			out += "span_end=" + m.Span.Start().Format(time.RFC3339Nano) + "\n"
		default:
			out += l + "\n"
		}
	}
	path := filepath.Join(t.TempDir(), "test.manifest")
	require.NoError(t, os.WriteFile(path, []byte(out), 0o644))
	_, err := readManifestFile(path, m.Instrument)
	assert.ErrorIs(t, err, errStoreMalformed)
}

func TestReadManifestFile_MalformedParentFields(t *testing.T) {
	m := validManifest(t)
	m.ResamplerVersion = "resampler-v1"
	m.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "parent-rev-1"}

	replacements := map[string]string{
		"parent_interval_unit=":  "parent_interval_unit=not-a-number\n",
		"parent_interval_count=": "parent_interval_count=not-a-number\n",
	}
	for prefix, replacement := range replacements {
		t.Run(prefix, func(t *testing.T) {
			lines := encodedManifestLines(t, m)
			var out string
			for _, l := range lines {
				if strings.HasPrefix(l, prefix) {
					out += replacement
				} else {
					out += l + "\n"
				}
			}
			path := filepath.Join(t.TempDir(), "test.manifest")
			require.NoError(t, os.WriteFile(path, []byte(out), 0o644))
			_, err := readManifestFile(path, m.Instrument)
			assert.ErrorIs(t, err, errStoreMalformed)
		})
	}
}

func TestReadManifestFile_MalformedLineNoEquals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.manifest")
	require.NoError(t, os.WriteFile(path, []byte("this line has no equals sign\n"), 0o644))
	_, err := readManifestFile(path, eurusd())
	assert.ErrorIs(t, err, errStoreMalformed)
}

func TestReadManifestFile_MissingFile(t *testing.T) {
	_, err := readManifestFile(filepath.Join(t.TempDir(), "missing.manifest"), eurusd())
	assert.Error(t, err)
}

func TestParseUint8_Error(t *testing.T) {
	_, err := parseUint8("not-a-number")
	assert.Error(t, err)
	_, err = parseUint8("999")
	assert.Error(t, err, "out of uint8 range")
}

func TestParseOptionalTime_Error(t *testing.T) {
	_, err := parseOptionalTime("not-a-time")
	assert.Error(t, err)
}

// nthCancelContext reports context.Canceled from Err() once its
// remaining count is exhausted, letting a test control precisely which
// of publish's several ctx.Err() checks first observes cancellation.
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

// Regression/documentation test for the exact trade-off publish's own
// doc comment describes: cancellation landing between the data rename
// and the manifest rename leaves new data paired with the old manifest.
// load must detect and reject that pair via Matches, not serve it.
func TestCanonicalCSVStore_PublishCancelledBetweenWrites(t *testing.T) {
	store := newCanonicalCSVStore(t.TempDir())
	key := validPartitionKey(t)
	m := validManifest(t)
	bs := validBarSet(t)
	require.NoError(t, store.publish(context.Background(), key, m, bs))

	// bs2 actually differs from bs (one fewer bar), so a manifest
	// describing bs while paired with bs2's data is a genuine,
	// detectable mismatch — unlike a change to BuilderVersion alone,
	// which Matches does not even inspect.
	bs2 := bs
	bs2.Bars = bs.Bars[:1]
	m2 := m
	m2.BarCount = 1
	m2.LastBar = bs2.Bars[0].Time
	require.NoError(t, m2.Matches(bs2))

	// Two ctx.Err() checks (top of publish, before the data write) must
	// pass; the third (before the manifest write) must observe
	// cancellation.
	ctx := &nthCancelContext{Context: context.Background(), remaining: 2}
	err := store.publish(ctx, key, m2, bs2)
	assert.ErrorIs(t, err, context.Canceled)

	_, _, err = store.load(context.Background(), key)
	assert.Error(t, err, "new data paired with the stale manifest must be rejected, not silently served")
}
