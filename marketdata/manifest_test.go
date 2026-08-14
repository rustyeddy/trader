package marketdata

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gbpusd() instrument.ID {
	return instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
}

// validManifest returns a well-formed manifest, paired with validBarSet's
// EUR/USD H1 span, for the tests to mutate.
func validManifest(t *testing.T) Manifest {
	t.Helper()
	start := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, 3, 2, 4, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(start, end)
	require.NoError(t, err)
	return Manifest{
		Provider:         "oanda",
		Instrument:       eurusd(),
		Interval:         H1,
		Span:             span,
		Basis:            BasisBid,
		SchemaVersion:    1,
		RawFingerprint:   "deadbeef",
		BuilderVersion:   "builder-v1",
		ValidatorVersion: "validator-v1",
		ResamplerVersion: "none",
		CalendarVersion:  "fxcalendar-v1",
		BuiltAt:          time.Date(2020, 3, 3, 0, 0, 0, 0, time.UTC),
		BarCount:         2,
		FirstBar:         start,
		LastBar:          start.Add(time.Hour),
	}
}

func TestManifestValidate_OK(t *testing.T) {
	require.NoError(t, validManifest(t).Validate())
}

func TestManifestValidate_ZeroBarCountAllowsZeroBarRange(t *testing.T) {
	m := validManifest(t)
	m.BarCount = 0
	m.FirstBar = time.Time{}
	m.LastBar = time.Time{}
	assert.NoError(t, m.Validate(), "an empty dataset over a span is valid")
}

func TestManifestValidate_EmptyProvider(t *testing.T) {
	m := validManifest(t)
	m.Provider = ""
	assert.ErrorIs(t, m.Validate(), ErrManifestProvider)
}

func TestManifestValidate_ZeroInstrument(t *testing.T) {
	m := validManifest(t)
	m.Instrument = instrument.ID{}
	assert.ErrorIs(t, m.Validate(), ErrManifestInstrument)
}

func TestManifestValidate_InvalidInterval(t *testing.T) {
	m := validManifest(t)
	m.Interval = Interval{}
	assert.ErrorIs(t, m.Validate(), ErrManifestInterval)
}

func TestManifestValidate_InvalidSpan(t *testing.T) {
	m := validManifest(t)
	m.Span = TimeRange{}
	assert.ErrorIs(t, m.Validate(), ErrManifestSpan)
}

func TestManifestValidate_UnknownBasis(t *testing.T) {
	m := validManifest(t)
	m.Basis = BasisUnknown
	assert.ErrorIs(t, m.Validate(), ErrManifestBasis)
}

func TestManifestValidate_EmptyFingerprint(t *testing.T) {
	m := validManifest(t)
	m.RawFingerprint = ""
	assert.ErrorIs(t, m.Validate(), ErrManifestFingerprint)
}

func TestManifestValidate_EmptyVersionStrings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"builder", func(m *Manifest) { m.BuilderVersion = "" }},
		{"validator", func(m *Manifest) { m.ValidatorVersion = "" }},
		{"resampler", func(m *Manifest) { m.ResamplerVersion = "" }},
		{"calendar", func(m *Manifest) { m.CalendarVersion = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := validManifest(t)
			tc.mutate(&m)
			assert.ErrorIs(t, m.Validate(), ErrManifestVersion)
		})
	}
}

func TestManifestValidate_ZeroBuiltAt(t *testing.T) {
	m := validManifest(t)
	m.BuiltAt = time.Time{}
	assert.ErrorIs(t, m.Validate(), ErrManifestBuiltAt)
}

func TestManifestValidate_NegativeBarCount(t *testing.T) {
	m := validManifest(t)
	m.BarCount = -1
	assert.ErrorIs(t, m.Validate(), ErrManifestBarCount)
}

func TestManifestValidate_BarRangeReversed(t *testing.T) {
	m := validManifest(t)
	m.FirstBar, m.LastBar = m.LastBar, m.FirstBar
	assert.ErrorIs(t, m.Validate(), ErrManifestBarRange)
}

func TestManifestValidate_BarRangeOutsideSpan(t *testing.T) {
	m := validManifest(t)
	m.LastBar = m.Span.end.Add(time.Hour)
	assert.ErrorIs(t, m.Validate(), ErrManifestBarRange)
}

func TestManifestValidate_BarCountPositiveButZeroFirstBar(t *testing.T) {
	m := validManifest(t)
	m.FirstBar = time.Time{}
	assert.ErrorIs(t, m.Validate(), ErrManifestBarRange)
}

func TestManifestValidate_ParentMissingRevision(t *testing.T) {
	m := validManifest(t)
	m.Parent = &ParentRef{Instrument: eurusd(), Interval: M1}
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ParentMissingInstrument(t *testing.T) {
	m := validManifest(t)
	m.Parent = &ParentRef{Interval: M1, Revision: "abc"}
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ParentInvalidInterval(t *testing.T) {
	m := validManifest(t)
	m.Parent = &ParentRef{Instrument: eurusd(), Revision: "abc"}
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ValidParentOK(t *testing.T) {
	m := validManifest(t)
	m.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "abc"}
	assert.NoError(t, m.Validate())
}

// --- Matches ---

func TestManifestMatches_OK(t *testing.T) {
	m := validManifest(t)
	bs := validBarSet(t)
	assert.NoError(t, m.Matches(bs))
}

func TestManifestMatches_InstrumentMismatch(t *testing.T) {
	m := validManifest(t)
	bs := validBarSet(t)
	bs.Instrument = gbpusd()
	assert.ErrorIs(t, m.Matches(bs), ErrManifestMismatch)
}

func TestManifestMatches_IntervalMismatch(t *testing.T) {
	m := validManifest(t)
	bs := validBarSet(t)
	bs.Interval = M1
	assert.ErrorIs(t, m.Matches(bs), ErrManifestMismatch)
}

func TestManifestMatches_BasisMismatch(t *testing.T) {
	m := validManifest(t)
	bs := validBarSet(t)
	bs.Basis = BasisAsk
	assert.ErrorIs(t, m.Matches(bs), ErrManifestMismatch)
}

func TestManifestMatches_SpanMismatch(t *testing.T) {
	m := validManifest(t)
	bs := validBarSet(t)
	span, err := NewTimeRange(bs.Span.start, bs.Span.end.Add(time.Hour))
	require.NoError(t, err)
	bs.Span = span
	assert.ErrorIs(t, m.Matches(bs), ErrManifestMismatch)
}

func TestManifestMatches_BarCountMismatch(t *testing.T) {
	m := validManifest(t)
	bs := validBarSet(t)
	bs.Bars = bs.Bars[:1]
	assert.ErrorIs(t, m.Matches(bs), ErrManifestMismatch)
}

// --- Revision ---

func TestManifestRevision_DeterministicForIdenticalValues(t *testing.T) {
	a := validManifest(t)
	b := validManifest(t)
	assert.Equal(t, a.Revision(), b.Revision())
}

func TestManifestRevision_ChangesPerField(t *testing.T) {
	base := validManifest(t)
	baseRev := base.Revision()

	mutations := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"provider", func(m *Manifest) { m.Provider = "alpaca" }},
		{"instrument", func(m *Manifest) { m.Instrument = gbpusd() }},
		{"interval", func(m *Manifest) { m.Interval = M1 }},
		{"span", func(m *Manifest) {
			span, err := NewTimeRange(m.Span.start, m.Span.end.Add(time.Hour))
			require.NoError(t, err)
			m.Span = span
		}},
		{"basis", func(m *Manifest) { m.Basis = BasisAsk }},
		{"schemaVersion", func(m *Manifest) { m.SchemaVersion = 2 }},
		{"rawFingerprint", func(m *Manifest) { m.RawFingerprint = "otherhash" }},
		{"builderVersion", func(m *Manifest) { m.BuilderVersion = "builder-v2" }},
		{"validatorVersion", func(m *Manifest) { m.ValidatorVersion = "validator-v2" }},
		{"resamplerVersion", func(m *Manifest) { m.ResamplerVersion = "resampler-v1" }},
		{"calendarVersion", func(m *Manifest) { m.CalendarVersion = "fxcalendar-v2" }},
		{"builtAt", func(m *Manifest) { m.BuiltAt = m.BuiltAt.Add(time.Hour) }},
		{"barCount", func(m *Manifest) { m.BarCount = 3; m.LastBar = m.LastBar.Add(time.Hour) }},
		{"firstBar", func(m *Manifest) { m.FirstBar = m.FirstBar.Add(time.Minute) }},
		{"lastBar", func(m *Manifest) { m.LastBar = m.LastBar.Add(time.Minute) }},
		{"parent", func(m *Manifest) {
			m.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "abc"}
		}},
	}

	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			m := validManifest(t)
			tc.mutate(&m)
			assert.NotEqual(t, baseRev, m.Revision(), "changing %s should change Revision", tc.name)
		})
	}
}

func TestManifestRevision_ParentRevisionAffectsHash(t *testing.T) {
	m1 := validManifest(t)
	m1.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "rev-1"}

	m2 := validManifest(t)
	m2.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "rev-2"}

	assert.NotEqual(t, m1.Revision(), m2.Revision())
}

func TestManifestRevision_NilParentDiffersFromSetParent(t *testing.T) {
	withoutParent := validManifest(t)
	withParent := validManifest(t)
	withParent.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "rev-1"}

	assert.NotEqual(t, withoutParent.Revision(), withParent.Revision())
}

func TestManifestRevision_HexEncodedSHA256Length(t *testing.T) {
	m := validManifest(t)
	// sha256 -> 32 bytes -> 64 hex characters.
	assert.Len(t, m.Revision(), 64)
}
