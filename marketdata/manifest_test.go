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

// validRawFingerprint is a well-formed "<algorithm>:<hex digest>" value,
// long enough to look like a real sha256 digest without needing to be
// one.
const validRawFingerprint = "sha256:" +
	"2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7a"

// validManifest returns a well-formed manifest, paired with validBarSet's
// EUR/USD H1 span, for the tests to mutate. It has no parent (built
// directly from raw), matching its "none" ResamplerVersion.
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
		RawFingerprint:   validRawFingerprint,
		BuilderVersion:   "builder-v1",
		ValidatorVersion: "validator-v1",
		ResamplerVersion: noResampler,
		CalendarVersion:  "fxcalendar-v1",
		BuiltAt:          time.Date(2020, 3, 3, 0, 0, 0, 0, time.UTC),
		BarCount:         2,
		FirstBar:         start,
		LastBar:          start.Add(time.Hour),
	}
}

// validDerivedManifest returns a well-formed manifest resampled from a
// finer parent dataset over the same instrument.
func validDerivedManifest(t *testing.T) Manifest {
	t.Helper()
	m := validManifest(t)
	m.ResamplerVersion = "resampler-v1"
	m.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "parent-rev-1"}
	return m
}

func TestManifestValidate_OK(t *testing.T) {
	require.NoError(t, validManifest(t).Validate())
}

func TestManifestValidate_DerivedOK(t *testing.T) {
	require.NoError(t, validDerivedManifest(t).Validate())
}

func TestManifestValidate_ZeroBarCountRequiresZeroBarRange(t *testing.T) {
	m := validManifest(t)
	m.BarCount = 0
	m.FirstBar = time.Time{}
	m.LastBar = time.Time{}
	assert.NoError(t, m.Validate(), "an empty dataset over a span is valid")
}

func TestManifestValidate_ZeroBarCountWithNonZeroRangeRejected(t *testing.T) {
	m := validManifest(t)
	m.BarCount = 0
	// FirstBar/LastBar left at the two-bar defaults: BarCount says empty,
	// but the coverage summary still claims a range.
	assert.ErrorIs(t, m.Validate(), ErrManifestBarRange)
}

func TestManifestValidate_SingleBarRequiresEqualFirstLast(t *testing.T) {
	m := validManifest(t)
	m.BarCount = 1
	// FirstBar/LastBar left unequal (the two-bar defaults).
	assert.ErrorIs(t, m.Validate(), ErrManifestBarRange)
}

func TestManifestValidate_SingleBarEqualFirstLastOK(t *testing.T) {
	m := validManifest(t)
	m.BarCount = 1
	m.LastBar = m.FirstBar
	assert.NoError(t, m.Validate())
}

func TestManifestValidate_SingleBarOutsideSpan(t *testing.T) {
	m := validManifest(t)
	m.BarCount = 1
	m.FirstBar = m.Span.end.Add(time.Hour)
	m.LastBar = m.FirstBar
	assert.ErrorIs(t, m.Validate(), ErrManifestBarRange)
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

func TestManifestValidate_SchemaVersionZero(t *testing.T) {
	m := validManifest(t)
	m.SchemaVersion = 0
	assert.ErrorIs(t, m.Validate(), ErrManifestSchemaVersion)
}

func TestManifestValidate_SchemaVersionNegative(t *testing.T) {
	m := validManifest(t)
	m.SchemaVersion = -1
	assert.ErrorIs(t, m.Validate(), ErrManifestSchemaVersion)
}

func TestManifestValidate_EmptyFingerprint(t *testing.T) {
	m := validManifest(t)
	m.RawFingerprint = ""
	assert.ErrorIs(t, m.Validate(), ErrManifestFingerprint)
}

func TestManifestValidate_MalformedFingerprint(t *testing.T) {
	for _, bad := range []string{
		"not hex",
		"deadbeef",         // no algorithm prefix
		"sha256:",          // empty digest
		"sha256:DEADBEEF",  // uppercase hex not accepted
		":deadbeef",        // empty algorithm
		"sha256:dead beef", // embedded space
	} {
		t.Run(bad, func(t *testing.T) {
			m := validManifest(t)
			m.RawFingerprint = bad
			assert.ErrorIs(t, m.Validate(), ErrManifestFingerprint)
		})
	}
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
	m.BarCount = 2
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

// --- Parent / resampler lineage ---

func TestManifestValidate_ResamplerSetWithoutParent(t *testing.T) {
	m := validManifest(t)
	m.ResamplerVersion = "resampler-v1"
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ParentSetWithoutResampler(t *testing.T) {
	m := validManifest(t)
	m.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "abc"}
	// ResamplerVersion still "none" from validManifest.
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ParentMissingRevision(t *testing.T) {
	m := validDerivedManifest(t)
	m.Parent.Revision = ""
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ParentMissingInstrument(t *testing.T) {
	m := validDerivedManifest(t)
	m.Parent.Instrument = instrument.ID{}
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ParentInvalidInterval(t *testing.T) {
	m := validDerivedManifest(t)
	m.Parent.Interval = Interval{}
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ParentDifferentInstrument(t *testing.T) {
	m := validDerivedManifest(t)
	m.Parent.Instrument = gbpusd()
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ParentSameInterval(t *testing.T) {
	m := validDerivedManifest(t)
	m.Parent.Interval = m.Interval // H1 parent for an H1 child
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ParentCoarserInterval(t *testing.T) {
	m := validDerivedManifest(t)
	m.Parent.Interval = D1 // coarser than the H1 child
	assert.ErrorIs(t, m.Validate(), ErrManifestParent)
}

func TestManifestValidate_ValidParentOK(t *testing.T) {
	assert.NoError(t, validDerivedManifest(t).Validate())
}

func TestManifestValidate_ParentDailyChildWeeklyOK(t *testing.T) {
	// Exercises the UnitWeek arm of the finer-than-coarser check: a D1
	// parent is finer than a W1 child.
	start := time.Date(2020, 3, 1, 17, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(start, start.Add(7*24*time.Hour))
	require.NoError(t, err)

	m := validManifest(t)
	m.Interval = W1
	m.Span = span
	m.BarCount = 0
	m.FirstBar = time.Time{}
	m.LastBar = time.Time{}
	m.ResamplerVersion = "resampler-v1"
	m.Parent = &ParentRef{Instrument: eurusd(), Interval: D1, Revision: "parent-rev-1"}

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

func TestManifestMatches_FirstBarMismatch(t *testing.T) {
	m := validManifest(t)
	m.FirstBar = m.FirstBar.Add(time.Minute)
	bs := validBarSet(t)
	assert.ErrorIs(t, m.Matches(bs), ErrManifestMismatch)
}

func TestManifestMatches_LastBarMismatch(t *testing.T) {
	m := validManifest(t)
	m.LastBar = m.LastBar.Add(time.Minute)
	bs := validBarSet(t)
	assert.ErrorIs(t, m.Matches(bs), ErrManifestMismatch)
}

func TestManifestMatches_EmptyBarSetNonZeroManifestRange(t *testing.T) {
	m := validManifest(t)
	m.BarCount = 0
	bs := validBarSet(t)
	bs.Bars = nil
	// m.FirstBar/LastBar are still set from validManifest's two-bar
	// defaults, so this must be reported even though BarCount agrees.
	assert.ErrorIs(t, m.Matches(bs), ErrManifestMismatch)
}

func TestManifestMatches_EmptyBarSetZeroManifestRangeOK(t *testing.T) {
	m := validManifest(t)
	m.BarCount = 0
	m.FirstBar = time.Time{}
	m.LastBar = time.Time{}
	bs := validBarSet(t)
	bs.Bars = nil
	assert.NoError(t, m.Matches(bs))
}

// --- Revision ---

func TestManifestRevision_DeterministicForIdenticalValues(t *testing.T) {
	a := validManifest(t)
	b := validManifest(t)
	assert.Equal(t, a.Revision(), b.Revision())
}

func TestManifestRevision_BuiltAtExcluded(t *testing.T) {
	a := validManifest(t)
	b := validManifest(t)
	b.BuiltAt = a.BuiltAt.Add(24 * time.Hour)
	assert.Equal(t, a.Revision(), b.Revision(),
		"BuiltAt is provenance, not identity, and must not affect Revision")
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
		{"rawFingerprint", func(m *Manifest) {
			m.RawFingerprint = "sha256:" + "0000000000000000000000000000000000000000000000000000000000000"
		}},
		{"builderVersion", func(m *Manifest) { m.BuilderVersion = "builder-v2" }},
		{"validatorVersion", func(m *Manifest) { m.ValidatorVersion = "validator-v2" }},
		{"resamplerVersion+parent", func(m *Manifest) {
			m.ResamplerVersion = "resampler-v1"
			m.Parent = &ParentRef{Instrument: eurusd(), Interval: M1, Revision: "abc"}
		}},
		{"calendarVersion", func(m *Manifest) { m.CalendarVersion = "fxcalendar-v2" }},
		{"barCount", func(m *Manifest) { m.BarCount = 3; m.LastBar = m.LastBar.Add(time.Hour) }},
		{"firstBar", func(m *Manifest) { m.FirstBar = m.FirstBar.Add(time.Minute) }},
		{"lastBar", func(m *Manifest) { m.LastBar = m.LastBar.Add(time.Minute) }},
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
	m1 := validDerivedManifest(t)
	m1.Parent.Revision = "rev-1"

	m2 := validDerivedManifest(t)
	m2.Parent.Revision = "rev-2"

	assert.NotEqual(t, m1.Revision(), m2.Revision())
}

func TestManifestRevision_NilParentDiffersFromSetParent(t *testing.T) {
	withoutParent := validManifest(t)
	withParent := validDerivedManifest(t)

	assert.NotEqual(t, withoutParent.Revision(), withParent.Revision())
}

func TestManifestRevision_NoDelimiterCollisionAcrossFields(t *testing.T) {
	// Regression test: a hand-built, newline-joined "key=value" encoding
	// let a suffix of one field bleed into the next field's key, so two
	// distinct (RawFingerprint, BuilderVersion) pairs could hash equal.
	// The JSON-based encoding must not have that problem.
	a := validManifest(t)
	a.RawFingerprint = "x\nbuilder=y"
	a.BuilderVersion = "z"

	b := validManifest(t)
	b.RawFingerprint = "x"
	b.BuilderVersion = "y\nbuilder=z"

	assert.NotEqual(t, a.Revision(), b.Revision())
}

func TestManifestRevision_HexEncodedSHA256Length(t *testing.T) {
	m := validManifest(t)
	// sha256 -> 32 bytes -> 64 hex characters.
	assert.Len(t, m.Revision(), 64)
}
