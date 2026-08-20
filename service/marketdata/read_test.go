package marketdata_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

// fixtureRawRoot is this package's own committed raw archive, copied
// from examples/m2's identical fixture (issue #82/M2-14): EUR/USD H1
// and D1 for two trading weeks starting 2024-01-07 (a Sunday reopen),
// with 2024-01-16 (a Tuesday) deliberately absent from both files.
// Kept as this package's own copy, rather than a relative reference
// into examples/m2, so this package's tests do not depend on another
// package's testdata layout.
const fixtureRawRoot = "testdata/raw/oanda"

func eurusdListing(t *testing.T) instrument.Listing {
	t.Helper()
	eurusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: eurusd,
		Provider:   "oanda",
		Symbol:     "EURUSD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func eurusdID(t *testing.T) instrument.ID {
	t.Helper()
	return eurusdListing(t).InstrumentID()
}

// copyFixtureRaw copies fixtureRawRoot into a fresh, writable temp
// directory so every test operates on its own copy, never the checked-in
// fixture itself.
func copyFixtureRaw(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(fixtureRawRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(fixtureRawRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	require.NoError(t, err)
	return dst
}

// newTestManagerAndService builds a *marketdata.Manager wired to a
// writable copy of this package's committed raw fixture and a fresh,
// empty canonical store, and a Service wrapping it, so every test
// starts from a clean, isolated slate. It returns both: tests exercise
// the Service under test, but some need the underlying Manager
// directly to publish canonical fixture data via Plan+Build — Service
// itself has no Build operation yet (issue #106), so that remains
// test-only setup, not something being tested here.
func newTestManagerAndService(t *testing.T) (*marketdata.Manager, *svc.Service) {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t)))

	mgr, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      copyFixtureRaw(t),
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)

	s, err := svc.New(mgr)
	require.NoError(t, err)
	return mgr, s
}

// newTestService is a convenience wrapper for tests that never need the
// underlying Manager directly.
func newTestService(t *testing.T) *svc.Service {
	t.Helper()
	_, s := newTestManagerAndService(t)
	return s
}

// fixtureSpan is the exact half-open range this package's raw fixture
// covers, matching examples/m2's identical fixture span.
func fixtureSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 7, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 19, 22, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

func datasetRequest(t *testing.T, interval marketdata.Interval, span marketdata.TimeRange) svc.DatasetRequest {
	t.Helper()
	return svc.DatasetRequest{
		Instrument: eurusdID(t),
		Interval:   interval,
		Range:      span,
	}
}

func TestPlan_ReportsRequiredWorkWithoutPerformingIt(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx := context.Background()

	resp, err := s.Plan(ctx, svc.PlanRequest{DatasetRequest: datasetRequest(t, marketdata.H1, fixtureSpan(t))})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Plan.Actions, "raw H1 is present but nothing is canonical yet")
	require.Equal(t, marketdata.ActionNormalizeCanonical, resp.Plan.Actions[0].Kind)

	// Plan must not have built or published anything: a direct Bars
	// query against the same dataset still finds nothing.
	_, err = s.Bars(ctx, svc.BarsRequest{DatasetRequest: datasetRequest(t, marketdata.H1, fixtureSpan(t))})
	require.ErrorIs(t, err, marketdata.ErrDataUnavailable)
}

func TestCoverage_ReportsGapsWithoutBuilding(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx := context.Background()

	resp, err := s.Coverage(ctx, svc.CoverageRequest{DatasetRequest: datasetRequest(t, marketdata.D1, fixtureSpan(t))})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Coverage.Partitions)
}

func TestBars_ReturnsPublishedCanonicalData(t *testing.T) {
	t.Parallel()
	mgr, s := newTestManagerAndService(t)
	ctx := context.Background()
	span := fixtureSpan(t)

	planResp, err := s.Plan(ctx, svc.PlanRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)

	buildResult, err := mgr.Build(ctx, planResp.Plan)
	require.NoError(t, err)
	require.Len(t, buildResult.Published, 1)

	barsResp, err := s.Bars(ctx, svc.BarsRequest{DatasetRequest: datasetRequest(t, marketdata.H1, span)})
	require.NoError(t, err)
	require.Len(t, barsResp.Bars, 216, "9 open days x 24 H1 bars, per this package's own fixture")
	require.Len(t, barsResp.Manifests, 1)
}

func TestBars_MissingDataReturnsErrDataUnavailable(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx := context.Background()

	_, err := s.Bars(ctx, svc.BarsRequest{DatasetRequest: datasetRequest(t, marketdata.D1, fixtureSpan(t))})
	require.ErrorIs(t, err, marketdata.ErrDataUnavailable)
}

func TestBars_InvalidRequestNeverReachesManager(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx := context.Background()

	_, err := s.Bars(ctx, svc.BarsRequest{})
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
}

func TestCoverage_InvalidRequestNeverReachesManager(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx := context.Background()

	_, err := s.Coverage(ctx, svc.CoverageRequest{})
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
}

func TestPlan_InvalidRequestNeverReachesManager(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx := context.Background()

	_, err := s.Plan(ctx, svc.PlanRequest{})
	require.ErrorIs(t, err, svc.ErrInvalidRequest)
}

func TestBars_PropagatesContextCancellation(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Bars(ctx, svc.BarsRequest{DatasetRequest: datasetRequest(t, marketdata.H1, fixtureSpan(t))})
	require.ErrorIs(t, err, context.Canceled)
}

func TestCoverage_PropagatesContextCancellation(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Coverage(ctx, svc.CoverageRequest{DatasetRequest: datasetRequest(t, marketdata.D1, fixtureSpan(t))})
	require.Error(t, err)
}

func TestPlan_PropagatesContextCancellation(t *testing.T) {
	t.Parallel()
	s := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Plan(ctx, svc.PlanRequest{DatasetRequest: datasetRequest(t, marketdata.D1, fixtureSpan(t))})
	require.Error(t, err)
}
