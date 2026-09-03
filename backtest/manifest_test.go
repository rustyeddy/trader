package backtest_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
)

func mustRunID(t *testing.T, seed uint64) id.RunID {
	t.Helper()
	c := clock.NewSimulated(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC))
	ids := id.NewGenerator(c, id.NewDeterministic(seed, seed+1))
	runID, err := id.GenerateRunID(ids)
	require.NoError(t, err)
	return runID
}

// mustDatasetManifest returns a well-formed marketdata.Manifest for
// eurusdID/H1 over mustManifestSpan, suitable as Manifest's own
// required Dataset provenance entry in tests.
func mustDatasetManifest(t *testing.T) marketdata.Manifest {
	t.Helper()
	return mustDatasetManifestFor(t, marketdata.H1, mustManifestSpan(t))
}

// mustDatasetManifestFor is mustDatasetManifest generalized over
// interval/span, for tests that need multiple distinct, individually
// well-formed dataset entries (canonical-ordering tie-break tests).
func mustDatasetManifestFor(t *testing.T, interval marketdata.Interval, span marketdata.TimeRange) marketdata.Manifest {
	t.Helper()
	m := marketdata.Manifest{
		Provider:         "oanda",
		Instrument:       eurusdID(t),
		Interval:         interval,
		Span:             span,
		Basis:            marketdata.BasisBid,
		SchemaVersion:    1,
		RawFingerprint:   "sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
		BuilderVersion:   "test-builder-v1",
		ValidatorVersion: "test-validator-v1",
		ResamplerVersion: "none",
		CalendarVersion:  "test-calendar-v1",
		BuiltAt:          time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, m.Validate())
	return m
}

func mustManifestSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

type testStrategyParams struct {
	FastPeriod int    `json:"fast_period"`
	SlowPeriod int    `json:"slow_period"`
	Mode       string `json:"mode"`
}

func baseManifestParams(t *testing.T) backtest.ManifestParams {
	t.Helper()
	fillModel, err := backtest.NewComponentInfo("bar-close", "v1", nil)
	require.NoError(t, err)
	slippageModel, err := backtest.NewComponentInfo("none", "", nil)
	require.NoError(t, err)
	commissionModel, err := backtest.NewComponentInfo("fixed", "v1", map[string]string{"rate": "0.0001"})
	require.NoError(t, err)

	return backtest.ManifestParams{
		RunID:           mustRunID(t, 1),
		StrategyName:    "ema-cross",
		StrategyVersion: "1.0.0",
		StrategyParameters: testStrategyParams{
			FastPeriod: 12,
			SlowPeriod: 26,
			Mode:       "long-only",
		},
		Universe: []strategy.DataRequirement{
			{Instrument: eurusdID(t), Interval: marketdata.H1, WarmupBars: 26},
		},
		Span:            mustManifestSpan(t),
		StartingCapital: num.MustParseMoney("100000", num.MustParseCurrency("USD")),
		RiskFraction:    num.MustParseRate("0.01"),
		AdverseDistance: num.MustParsePrice("0.01000"),
		FillModel:       fillModel,
		SlippageModel:   slippageModel,
		CommissionModel: commissionModel,
		Dataset:         []marketdata.Manifest{mustDatasetManifest(t)},
	}
}

func TestNewManifest_RequiresRunID(t *testing.T) {
	p := baseManifestParams(t)
	p.RunID = id.RunID{}
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}

func TestNewManifest_RequiresStrategyName(t *testing.T) {
	p := baseManifestParams(t)
	p.StrategyName = ""
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}

func TestNewManifest_RequiresNonEmptyUniverse(t *testing.T) {
	p := baseManifestParams(t)
	p.Universe = nil
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}

func TestNewManifest_RequiresValidSpan(t *testing.T) {
	p := baseManifestParams(t)
	p.Span = marketdata.TimeRange{}
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}

func TestNewManifest_RequiresValidStartingCapital(t *testing.T) {
	p := baseManifestParams(t)
	p.StartingCapital = num.Money{}
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}

func TestManifest_AccessorsReturnConstructedValues(t *testing.T) {
	p := baseManifestParams(t)
	m, err := backtest.NewManifest(p)
	require.NoError(t, err)

	assert.True(t, m.RunID().Equal(p.RunID))
	assert.Equal(t, "ema-cross", m.StrategyName())
	assert.Equal(t, "1.0.0", m.StrategyVersion())
	assert.JSONEq(t, `{"fast_period":12,"slow_period":26,"mode":"long-only"}`, string(m.StrategyParameters()))
	require.Len(t, m.Universe(), 1)
	assert.True(t, m.Universe()[0].Instrument.Equal(eurusdID(t)))
	assert.Equal(t, p.StartingCapital, m.StartingCapital())
}

// TestManifest_UniverseOrderIsCanonicalRegardlessOfInputOrder proves
// two Manifests built from the same requirement set in different
// input orders are equal — a caller's own construction order must
// never leak into the manifest's own identity.
func TestManifest_UniverseOrderIsCanonicalRegardlessOfInputOrder(t *testing.T) {
	gbpusdInst, err := instrument.NewCurrencyPair(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	gbpusd := gbpusdInst.ID()

	p1 := baseManifestParams(t)
	p1.Universe = []strategy.DataRequirement{
		{Instrument: eurusdID(t), Interval: marketdata.H1},
		{Instrument: gbpusd, Interval: marketdata.H1},
	}
	p2 := baseManifestParams(t)
	p2.RunID = p1.RunID
	p2.Universe = []strategy.DataRequirement{
		{Instrument: gbpusd, Interval: marketdata.H1},
		{Instrument: eurusdID(t), Interval: marketdata.H1},
	}

	m1, err := backtest.NewManifest(p1)
	require.NoError(t, err)
	m2, err := backtest.NewManifest(p2)
	require.NoError(t, err)

	assert.Equal(t, m1.Universe(), m2.Universe())
	assert.True(t, m1.Equal(m2))
	assert.Equal(t, m1.ConfigDigest(), m2.ConfigDigest())
}

// TestManifest_ConfigDigestIgnoresRunID proves ConfigDigest recognizes
// two runs with identical configuration but different RunIDs as the
// same configuration (issue #215 review's "separate run identity from
// reproducibility identity").
func TestManifest_ConfigDigestIgnoresRunID(t *testing.T) {
	p1 := baseManifestParams(t)
	p2 := baseManifestParams(t)
	p2.RunID = mustRunID(t, 99)
	require.False(t, p1.RunID.Equal(p2.RunID))

	m1, err := backtest.NewManifest(p1)
	require.NoError(t, err)
	m2, err := backtest.NewManifest(p2)
	require.NoError(t, err)

	assert.Equal(t, m1.ConfigDigest(), m2.ConfigDigest())
	assert.False(t, m1.Equal(m2), "Equal considers RunID; ConfigDigest deliberately does not")
}

// TestManifest_ConfigDigestChangesWithConfiguration proves
// ConfigDigest is sensitive to an actual configuration difference, not
// a constant.
func TestManifest_ConfigDigestChangesWithConfiguration(t *testing.T) {
	p1 := baseManifestParams(t)
	p2 := baseManifestParams(t)
	p2.RunID = p1.RunID
	p2.RiskFraction = num.MustParseRate("0.02")

	m1, err := backtest.NewManifest(p1)
	require.NoError(t, err)
	m2, err := backtest.NewManifest(p2)
	require.NoError(t, err)

	assert.NotEqual(t, m1.ConfigDigest(), m2.ConfigDigest())
}

// TestManifest_MarshalJSONIsDeterministic proves serialization is
// byte-for-byte identical across independently constructed Manifests
// describing the same logical inputs.
func TestManifest_MarshalJSONIsDeterministic(t *testing.T) {
	p := baseManifestParams(t)

	m1, err := backtest.NewManifest(p)
	require.NoError(t, err)
	m2, err := backtest.NewManifest(p)
	require.NoError(t, err)

	data1, err := json.Marshal(m1)
	require.NoError(t, err)
	data2, err := json.Marshal(m2)
	require.NoError(t, err)
	assert.Equal(t, data1, data2)
}

// TestManifest_AccessorsReturnDefensiveCopies proves mutating a slice
// or byte value returned by an accessor never affects the Manifest's
// own internal state.
func TestManifest_AccessorsReturnDefensiveCopies(t *testing.T) {
	p := baseManifestParams(t)
	m, err := backtest.NewManifest(p)
	require.NoError(t, err)

	universe := m.Universe()
	universe[0] = strategy.DataRequirement{}

	params := m.StrategyParameters()
	for i := range params {
		params[i] = 'x'
	}

	assert.True(t, m.Universe()[0].Instrument.Equal(eurusdID(t)), "mutating a returned slice must not affect the Manifest")
	assert.JSONEq(t, `{"fast_period":12,"slow_period":26,"mode":"long-only"}`, string(m.StrategyParameters()))
}

func TestComponentInfo_RequiresName(t *testing.T) {
	_, err := backtest.NewComponentInfo("", "v1", nil)
	require.ErrorIs(t, err, backtest.ErrInvalidComponentInfo)
}

func TestComponentInfo_CanonicalizesParameters(t *testing.T) {
	a, err := backtest.NewComponentInfo("fixed", "v1", map[string]int{"b": 2, "a": 1})
	require.NoError(t, err)
	b, err := backtest.NewComponentInfo("fixed", "v1", map[string]int{"a": 1, "b": 2})
	require.NoError(t, err)
	assert.True(t, a.Equal(b), "Go's own json.Marshal sorts map keys, so field-order in the caller's input must not matter")
}

func TestComponentInfo_ParametersReturnsDefensiveCopy(t *testing.T) {
	c, err := backtest.NewComponentInfo("fixed", "v1", map[string]int{"a": 1})
	require.NoError(t, err)
	params := c.Parameters()
	for i := range params {
		params[i] = 'x'
	}
	assert.JSONEq(t, `{"a":1}`, string(c.Parameters()))
}

// TestManifest_RemainingAccessors covers every accessor not already
// exercised elsewhere, and ComponentInfo's own Name/Version.
func TestManifest_RemainingAccessors(t *testing.T) {
	p := baseManifestParams(t)
	p.RiskRules = []backtest.ComponentInfo{p.FillModel} // reuse a valid ComponentInfo as a stand-in rule
	p.Dataset = []marketdata.Manifest{mustDatasetManifest(t)}
	p.TraderVersion = "v0.1.0-test"

	m, err := backtest.NewManifest(p)
	require.NoError(t, err)

	assert.Equal(t, p.Span, m.Span())
	assert.True(t, m.RiskFraction().Equal(p.RiskFraction))
	assert.True(t, m.AdverseDistance().Equal(p.AdverseDistance))
	require.Len(t, m.RiskRules(), 1)
	assert.Equal(t, "bar-close", m.RiskRules()[0].Name())
	assert.Equal(t, "v1", m.RiskRules()[0].Version())
	assert.Equal(t, p.FillModel.Name(), m.FillModel().Name())
	assert.Equal(t, p.SlippageModel.Name(), m.SlippageModel().Name())
	assert.Equal(t, p.CommissionModel.Name(), m.CommissionModel().Name())
	require.Len(t, m.Dataset(), 1)
	assert.True(t, m.Dataset()[0].Instrument.Equal(eurusdID(t)))
	assert.Equal(t, "v0.1.0-test", m.TraderVersion())
}

// TestManifest_UniverseSortsByIntervalThenComponentsSortByVersion
// exercises the remaining tie-break branches in dataRequirementLess,
// componentInfoLess, and datasetManifestLess: same instrument/name,
// differing interval/version/span.
func TestManifest_UniverseSortsByIntervalThenComponentsSortByVersion(t *testing.T) {
	d1, err := backtest.NewComponentInfo("shared-name", "v2", nil)
	require.NoError(t, err)
	h1, err := backtest.NewComponentInfo("shared-name", "v1", nil)
	require.NoError(t, err)

	earlierSpan := mustManifestSpan(t)
	laterSpan, err := marketdata.NewTimeRange(
		earlierSpan.End(),
		earlierSpan.End().Add(24*time.Hour),
	)
	require.NoError(t, err)

	p := baseManifestParams(t)
	p.Universe = []strategy.DataRequirement{
		{Instrument: eurusdID(t), Interval: marketdata.D1},
		{Instrument: eurusdID(t), Interval: marketdata.H1},
	}
	p.RiskRules = []backtest.ComponentInfo{d1, h1}
	p.Dataset = []marketdata.Manifest{
		mustDatasetManifestFor(t, marketdata.H1, laterSpan),
		mustDatasetManifestFor(t, marketdata.H1, earlierSpan),
	}

	m, err := backtest.NewManifest(p)
	require.NoError(t, err)

	universe := m.Universe()
	require.Len(t, universe, 2)
	assert.Equal(t, marketdata.H1, universe[0].Interval, "H1 sorts before D1 by intrinsic Unit ordering")
	assert.Equal(t, marketdata.D1, universe[1].Interval)

	rules := m.RiskRules()
	require.Len(t, rules, 2)
	assert.Equal(t, "v1", rules[0].Version(), "same name: sorted by version")
	assert.Equal(t, "v2", rules[1].Version())

	dataset := m.Dataset()
	require.Len(t, dataset, 2)
	assert.True(t, dataset[0].Span.Start().Equal(earlierSpan.Start()), "same instrument/interval: sorted by span start")
	assert.True(t, dataset[1].Span.Start().Equal(laterSpan.Start()))

	summaries := m.DatasetSummaries()
	require.Len(t, summaries, len(dataset))
	for i, ds := range dataset {
		assert.Equal(t, ds.Provider, summaries[i].Provider)
		assert.Equal(t, ds.Instrument, summaries[i].Instrument)
		assert.Equal(t, ds.Interval.String(), summaries[i].Interval)
		assert.True(t, ds.Span.Start().Equal(summaries[i].SpanStart))
		assert.True(t, ds.Span.End().Equal(summaries[i].SpanEnd))
		assert.Equal(t, ds.Revision(), summaries[i].Revision)
	}
}

// TestManifest_RiskRuleParametersAreFinalTieBreak proves two risk
// rules sharing name/version but differing parameters produce
// identical canonical ordering (and therefore identical serialization/
// digest) regardless of caller input order — the specific gap #215's
// review found in componentInfoLess.
func TestManifest_RiskRuleParametersAreFinalTieBreak(t *testing.T) {
	a, err := backtest.NewComponentInfo("shared", "v1", map[string]int{"limit": 1})
	require.NoError(t, err)
	b, err := backtest.NewComponentInfo("shared", "v1", map[string]int{"limit": 2})
	require.NoError(t, err)

	p1 := baseManifestParams(t)
	p1.RiskRules = []backtest.ComponentInfo{a, b}
	p2 := baseManifestParams(t)
	p2.RunID = p1.RunID
	p2.RiskRules = []backtest.ComponentInfo{b, a}

	m1, err := backtest.NewManifest(p1)
	require.NoError(t, err)
	m2, err := backtest.NewManifest(p2)
	require.NoError(t, err)

	require.Len(t, m1.RiskRules(), 2)
	require.Len(t, m2.RiskRules(), 2)
	assert.JSONEq(t, string(m1.RiskRules()[0].Parameters()), string(m2.RiskRules()[0].Parameters()))
	assert.JSONEq(t, string(m1.RiskRules()[1].Parameters()), string(m2.RiskRules()[1].Parameters()))
	assert.Equal(t, m1.ConfigDigest(), m2.ConfigDigest())

	data1, err := json.Marshal(m1)
	require.NoError(t, err)
	data2, err := json.Marshal(m2)
	require.NoError(t, err)
	assert.Equal(t, data1, data2)
}

// TestNewManifest_RejectsDuplicateUniverseRequirement proves a
// duplicate (instrument, interval) pair in Universe is rejected
// outright rather than left for the canonical sort to disambiguate.
func TestNewManifest_RejectsDuplicateUniverseRequirement(t *testing.T) {
	p := baseManifestParams(t)
	p.Universe = []strategy.DataRequirement{
		{Instrument: eurusdID(t), Interval: marketdata.H1, WarmupBars: 0},
		{Instrument: eurusdID(t), Interval: marketdata.H1, WarmupBars: 10},
	}
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrDuplicateRequirement)
}

func TestNewManifest_RequiresFillModel(t *testing.T) {
	p := baseManifestParams(t)
	p.FillModel = backtest.ComponentInfo{}
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}

func TestNewManifest_RequiresSlippageModel(t *testing.T) {
	p := baseManifestParams(t)
	p.SlippageModel = backtest.ComponentInfo{}
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}

func TestNewManifest_RequiresCommissionModel(t *testing.T) {
	p := baseManifestParams(t)
	p.CommissionModel = backtest.ComponentInfo{}
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}

func TestNewManifest_RequiresNonEmptyDataset(t *testing.T) {
	p := baseManifestParams(t)
	p.Dataset = nil
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}

func TestNewManifest_RejectsInvalidDatasetEntry(t *testing.T) {
	p := baseManifestParams(t)
	p.Dataset = []marketdata.Manifest{{}} // zero value fails Validate
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}

func TestNewManifest_RejectsUnnamedRiskRule(t *testing.T) {
	p := baseManifestParams(t)
	p.RiskRules = []backtest.ComponentInfo{{}}
	_, err := backtest.NewManifest(p)
	require.ErrorIs(t, err, backtest.ErrInvalidManifest)
}
