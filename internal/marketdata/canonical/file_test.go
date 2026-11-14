package canonical

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/marketdata/layout"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKey_PathIsTheLayoutPath(t *testing.T) {
	root := t.TempDir()

	got, err := jan24H1().Path(root)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "oanda", "canonical", "EURUSD", "2024", "01", "EURUSD-2024-01-h1.csv"), got)
}

func TestKey_Validate(t *testing.T) {
	bad := func(mutate func(*Key)) Key { k := jan24H1(); mutate(&k); return k }
	assert.NoError(t, jan24H1().Validate())
	assert.ErrorIs(t, bad(func(k *Key) { k.Interval = "h4" }).Validate(), ErrUnsupportedInterval)
	assert.ErrorIs(t, bad(func(k *Key) { k.Symbol = "../x" }).Validate(), layout.ErrInvalidComponent)
	assert.ErrorIs(t, bad(func(k *Key) { k.Source = "" }).Validate(), layout.ErrInvalidComponent)
	assert.ErrorIs(t, bad(func(k *Key) { k.Month = 13 }).Validate(), layout.ErrInvalidComponent)
	_, err := bad(func(k *Key) { k.Interval = "w1" }).Path(t.TempDir())
	assert.ErrorIs(t, err, ErrUnsupportedInterval)
}

func TestMeta_SameBuildIgnoresOnlyTheBuildTime(t *testing.T) {
	a := testMeta()
	b := a
	b.BuiltAt = a.BuiltAt.Add(time.Hour)
	assert.True(t, a.SameBuild(b))
	for name, mutate := range map[string]func(*Meta){
		"fingerprint": func(m *Meta) { m.RawFingerprint = "sha256:other" },
		"builder":     func(m *Meta) { m.BuilderVersion = "builder-v2" },
		"validator":   func(m *Meta) { m.ValidatorVersion = "validator-v2" },
		"policy":      func(m *Meta) { m.AvailabilityPolicy = "scheduled-close-v2" },
	} {
		t.Run(name, func(t *testing.T) {
			c := a
			mutate(&c)
			assert.False(t, a.SameBuild(c))
		})
	}
}

func TestReason_Valid(t *testing.T) {
	for _, r := range []Reason{ReasonIncomplete, ReasonCrossed, ReasonInvalid, ReasonDuplicate, ReasonOutOfOrder, ReasonMisaligned} {
		assert.True(t, r.Valid(), string(r))
	}
	assert.False(t, Reason("").Valid())
	assert.False(t, Reason("weird").Valid())
}

func TestFile_Clean(t *testing.T) {
	f := goodFile(t)
	assert.True(t, f.Clean())
	f.Bad = []BadRecord{{Time: utc(2024, 1, 2, 9), Reason: ReasonCrossed, RawRow: 4}}
	assert.False(t, f.Clean(), "a file with a bad record is not clean")
}

func TestFile_Validate(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*File)
		want   error
	}{
		"valid":          {func(*File) {}, nil},
		"no bars at all": {func(f *File) { f.Bars = nil }, nil},
		"unknown policy": {func(f *File) {
			f.Meta.AvailabilityPolicy = "other-v9"
			f.Bars[0].AvailableAt = f.Bars[0].Time.Add(time.Minute)
		}, nil},
		"bad key":        {func(f *File) { f.Key.Interval = "h4" }, ErrUnsupportedInterval},
		"no fingerprint": {func(f *File) { f.Meta.RawFingerprint = "" }, ErrMalformedData},
		"no builder":     {func(f *File) { f.Meta.BuilderVersion = "" }, ErrMalformedData},
		"no validator":   {func(f *File) { f.Meta.ValidatorVersion = "" }, ErrMalformedData},
		"no policy":      {func(f *File) { f.Meta.AvailabilityPolicy = "" }, ErrMalformedData},
		"no build time":  {func(f *File) { f.Meta.BuiltAt = time.Time{} }, ErrMalformedData},
		"trade series": {func(f *File) {
			f.Bars[1].Trade = marketdata.OHLC{Open: px("1.1"), High: px("1.2"), Low: px("1.0"), Close: px("1.15")}
		}, ErrMalformedData},
		"invalid bar":          {func(f *File) { f.Bars[1].Volume = -5 }, ErrMalformedData},
		"bar in another month": {func(f *File) { f.Bars[2] = goodBar(t, utc(2024, 2, 1, 0)) }, ErrMalformedData},
		"bars not ascending":   {func(f *File) { f.Bars[1], f.Bars[2] = f.Bars[2], f.Bars[1] }, ErrMalformedData},
		"duplicate bar time":   {func(f *File) { f.Bars[2] = f.Bars[1] }, ErrMalformedData},
		"misaligned bar":       {func(f *File) { f.Bars[1] = goodBarAt(t, utc(2024, 1, 2, 1).Add(time.Minute)) }, ErrMalformedData},
		"wrong availability":   {func(f *File) { f.Bars[0].AvailableAt = f.Bars[0].Time.Add(2 * time.Hour) }, ErrMalformedData},
		"bad reason":           {func(f *File) { f.Bad = []BadRecord{{Time: utc(2024, 1, 3, 0), Reason: "nope", RawRow: 1}} }, ErrMalformedData},
		"bad row zero":         {func(f *File) { f.Bad = []BadRecord{{Time: utc(2024, 1, 3, 0), Reason: ReasonCrossed, RawRow: 0}} }, ErrMalformedData},
		"bad rows not ascending": {func(f *File) {
			f.Bad = []BadRecord{{Time: utc(2024, 1, 3, 0), Reason: ReasonCrossed, RawRow: 5}, {Time: utc(2024, 1, 3, 1), Reason: ReasonCrossed, RawRow: 5}}
		}, ErrMalformedData},
		"bad record in another month": {func(f *File) { f.Bad = []BadRecord{{Time: utc(2024, 3, 3, 0), Reason: ReasonCrossed, RawRow: 1}} }, ErrMalformedData},
		"bad records allowed": {func(f *File) {
			f.Bad = []BadRecord{{Time: utc(2024, 1, 3, 0), Reason: ReasonCrossed, RawRow: 7}, {Time: utc(2024, 1, 3, 0), Reason: ReasonDuplicate, RawRow: 8}}
		}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			f := goodFile(t)
			tc.mutate(&f)

			err := f.Validate()

			if tc.want == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

// goodBarAt is a bar opening at open with no alignment adjustment, for building
// deliberately misaligned input.
func goodBarAt(t *testing.T, open time.Time) marketdata.Bar {
	t.Helper()
	b := goodBar(t, utc(2024, 1, 2, 0))
	b.Time = open
	b.AvailableAt = open.Add(time.Hour)
	return b
}

func TestFile_ValidateD1UsesTheNewYorkSessionRule(t *testing.T) {
	open := utc(2024, 3, 9, 22) // Fri 17:00 EST
	avail, err := AvailableAt(D1, open)
	require.NoError(t, err)
	b := goodBar(t, utc(2024, 3, 9, 0))
	b.Time, b.AvailableAt = open, avail
	f := File{Key: Key{Source: "oanda", Symbol: "EURUSD", Interval: D1, Year: 2024, Month: time.March}, Meta: testMeta(), Bars: []marketdata.Bar{b}}
	require.NoError(t, f.Validate())

	f.Bars[0].AvailableAt = open.Add(24 * time.Hour) // the fixed-duration mistake
	assert.ErrorIs(t, f.Validate(), ErrMalformedData)
}
