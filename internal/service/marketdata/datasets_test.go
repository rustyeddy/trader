package marketdata_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/internal/logging"
	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
)

// datasetsService builds a resolving Service for provider at now, with
// rawRoot and Service options.
func datasetsService(t *testing.T, provider string, now time.Time, rawRoot string, opts ...svc.Option) *svc.Service {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	cfg := marketruntime.Config{
		Clock: clock.NewSimulated(now), StoreRoot: t.TempDir(), RawRoot: rawRoot,
		Resolver: resolver, ProviderName: provider,
	}
	if provider != "oanda" {
		cfg.Calendar = marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(2020))
	}
	manager, err := marketruntime.New(cfg)
	require.NoError(t, err)
	s, err := svc.New(manager, nil, append([]svc.Option{svc.WithResolver(resolver)}, opts...)...)
	require.NoError(t, err)
	return s
}

func symbols(names ...string) []svc.InstrumentRequest {
	out := make([]svc.InstrumentRequest, len(names))
	for i, n := range names {
		out[i] = svc.InstrumentRequest{Symbol: n}
	}
	return out
}

func d(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

const spyJanuary = stooqHeader + "SPY.US,D,20200131,000000,100,101,99,100.5,1000,0\n"

func TestDatasetsRequestValidate(t *testing.T) {
	span, err := marketdata.NewTimeRange(d(2020, 1, 1), d(2020, 2, 1))
	require.NoError(t, err)
	valid := svc.DatasetsRequest{Instruments: symbols("SPY"), Interval: marketdata.D1}
	require.NoError(t, valid.Validate())
	withRange := valid
	withRange.Range = span
	require.NoError(t, withRange.Validate())

	for name, r := range map[string]svc.DatasetsRequest{
		"no instruments":   {Interval: marketdata.D1},
		"invalid interval": {Instruments: symbols("SPY")},
	} {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, r.Validate(), svc.ErrInvalidRequest)
		})
	}
}

func TestCanonicalizeDatasets_StooqArchive(t *testing.T) {
	ctx := context.Background()
	archiveRoot := t.TempDir()
	writeZIP(t, filepath.Join(archiveRoot, "spy_us_d.zip"), map[string]string{"spy.us.txt": spyTwoMonths})
	s := datasetsService(t, "stooq", d(2020, 3, 1), t.TempDir(), svc.WithArchiveRoot(archiveRoot))
	req := svc.DatasetsRequest{Instruments: symbols("SPY", "spy", "MSFT"), Interval: marketdata.D1}

	resp, err := s.CanonicalizeDatasets(ctx, req, false)
	require.NoError(t, err, "one failing symbol does not fail the call")
	require.Len(t, resp.Results, 3)

	spy := resp.Results[0]
	require.NoError(t, spy.Err)
	assert.Equal(t, svc.DatasetBuilt, spy.Status)
	assert.True(t, spy.Instrument.Equal(instrument.ETFID("ARCA", "SPY")))
	assert.Equal(t, 2, spy.PublishedPartitions)
	assert.Equal(t, 2, spy.PublishedBars)
	assert.True(t, spy.Range.Start().Equal(d(2020, 1, 31)) && spy.Range.End().Equal(d(2020, 2, 4)), "the whole archive: %v", spy.Range)
	assert.Nil(t, spy.CanonicalBefore)
	require.NotNil(t, spy.CanonicalAfter)
	assert.True(t, spy.CanonicalAfter.Last.Equal(d(2020, 2, 3)))
	require.NotNil(t, spy.Raw)
	assert.Equal(t, 2, spy.Raw.Partitions)

	assert.Equal(t, svc.DatasetCurrent, resp.Results[1].Status, "the repeated symbol is already current")
	require.NoError(t, resp.Results[1].Err)

	assert.Equal(t, svc.DatasetFailed, resp.Results[2].Status)
	assert.ErrorIs(t, resp.Results[2].Err, svc.ErrNoListingDefault)
	assert.True(t, resp.Results[2].Instrument.IsZero())

	forced, err := s.CanonicalizeDatasets(ctx, svc.DatasetsRequest{Instruments: symbols("SPY"), Interval: marketdata.D1}, true)
	require.NoError(t, err)
	assert.Equal(t, svc.DatasetBuilt, forced.Results[0].Status, "force rebuilds current partitions")
	require.NotNil(t, forced.Results[0].CanonicalBefore)
}

func TestCanonicalizeDatasets_StooqExplicitRange(t *testing.T) {
	archiveRoot := t.TempDir()
	writeZIP(t, filepath.Join(archiveRoot, "spy.zip"), map[string]string{"spy.us.txt": spyTwoMonths})
	s := datasetsService(t, "stooq", d(2020, 3, 1), t.TempDir(), svc.WithArchiveRoot(archiveRoot))
	span, err := marketdata.NewTimeRange(d(2020, 1, 1), d(2020, 2, 1))
	require.NoError(t, err)

	resp, err := s.CanonicalizeDatasets(context.Background(), svc.DatasetsRequest{Instruments: symbols("SPY"), Interval: marketdata.D1, Range: span}, false)
	require.NoError(t, err)
	res := resp.Results[0]
	require.NoError(t, res.Err)
	assert.Equal(t, 1, res.PublishedPartitions)
	assert.True(t, res.Range.Start().Equal(d(2020, 1, 31)), "clipped to the source: %v", res.Range)
	assert.True(t, res.Range.End().Equal(d(2020, 2, 1)))
}

func TestCanonicalizeDatasets_StooqFallsBackToImportedRaw(t *testing.T) {
	ctx := context.Background()
	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "spy.zip")
	writeZIP(t, archivePath, map[string]string{"spy.us.txt": spyTwoMonths})
	s := datasetsService(t, "stooq", d(2020, 3, 1), t.TempDir(), svc.WithArchiveRoot(archiveDir))
	_, err := s.CanonicalizeDatasets(ctx, svc.DatasetsRequest{Instruments: symbols("SPY"), Interval: marketdata.D1}, false)
	require.NoError(t, err)
	require.NoError(t, os.Remove(archivePath))

	resp, err := s.CanonicalizeDatasets(ctx, svc.DatasetsRequest{Instruments: symbols("SPY"), Interval: marketdata.D1}, true)
	require.NoError(t, err)
	res := resp.Results[0]
	require.NoError(t, res.Err, "no archive, but the imported raw data is still there")
	assert.Equal(t, svc.DatasetBuilt, res.Status)
	assert.True(t, res.Range.Start().Equal(d(2020, 1, 31)) && res.Range.End().Equal(d(2020, 2, 4)), "[Raw.First, Raw.End): %v", res.Range)
}

func TestCanonicalizeDatasets_StooqNoArchiveNoRaw(t *testing.T) {
	s := datasetsService(t, "stooq", d(2020, 3, 1), t.TempDir(), svc.WithArchiveRoot(t.TempDir()))
	resp, err := s.CanonicalizeDatasets(context.Background(), svc.DatasetsRequest{Instruments: symbols("SPY"), Interval: marketdata.D1}, false)
	require.NoError(t, err)
	assert.Equal(t, svc.DatasetFailed, resp.Results[0].Status)
	assert.ErrorIs(t, resp.Results[0].Err, svc.ErrArchiveNotFound)
}

func TestCanonicalizeDatasets_RawProvider(t *testing.T) {
	ctx := context.Background()
	s := datasetsService(t, "oanda", d(2024, 2, 1), copyFixtureRaw(t))

	resp, err := s.CanonicalizeDatasets(ctx, svc.DatasetsRequest{Instruments: symbols("EURUSD", "GBPUSD"), Interval: marketdata.H1}, false)
	require.NoError(t, err)
	eur := resp.Results[0]
	require.NoError(t, eur.Err)
	assert.Equal(t, svc.DatasetBuilt, eur.Status)
	require.NotNil(t, eur.Raw)
	assert.True(t, eur.Range.Start().Equal(eur.Raw.First), "defaults to the whole raw span: %v", eur.Range)
	assert.True(t, eur.Range.End().Equal(eur.Raw.End))
	assert.Positive(t, eur.PublishedBars)
	require.NotNil(t, eur.CanonicalAfter)

	assert.Equal(t, svc.DatasetFailed, resp.Results[1].Status)
	assert.ErrorIs(t, resp.Results[1].Err, svc.ErrNoRawData)

	// Over a complete month, a second run has nothing to publish. (The
	// fixture's February ends in an in-progress record, which Plan
	// schedules for an "extend" rebuild on every run until raw catches
	// up, so the full-span default would still publish February.)
	complete := svc.DatasetsRequest{Instruments: symbols("EURUSD"), Interval: marketdata.H1, Range: fixtureSpan(t)}
	again, err := s.CanonicalizeDatasets(ctx, complete, false)
	require.NoError(t, err)
	require.NoError(t, again.Results[0].Err)
	assert.Equal(t, svc.DatasetCurrent, again.Results[0].Status)
	assert.Zero(t, again.Results[0].PublishedPartitions)
}

func TestUpdateDatasets_StooqPicksUpNewerArchiveData(t *testing.T) {
	ctx := context.Background()
	archiveRoot := t.TempDir()
	archivePath := filepath.Join(archiveRoot, "spy_us_d.zip")
	writeZIP(t, archivePath, map[string]string{"spy.us.txt": spyJanuary})
	s := datasetsService(t, "stooq", d(2020, 3, 1), t.TempDir(), svc.WithArchiveRoot(archiveRoot))
	req := svc.DatasetsRequest{Instruments: symbols("SPY"), Interval: marketdata.D1}
	_, err := s.CanonicalizeDatasets(ctx, req, false)
	require.NoError(t, err)

	current, err := s.UpdateDatasets(ctx, req)
	require.NoError(t, err)
	require.NoError(t, current.Results[0].Err)
	assert.Equal(t, svc.DatasetCurrent, current.Results[0].Status, "the archive has nothing newer")

	writeZIP(t, archivePath, map[string]string{"spy.us.txt": spyTwoMonths})
	resp, err := s.UpdateDatasets(ctx, req)
	require.NoError(t, err)
	res := resp.Results[0]
	require.NoError(t, res.Err)
	assert.Equal(t, svc.DatasetUpdated, res.Status)
	assert.True(t, res.Range.Start().Equal(d(2020, 1, 31)), "starts at the last canonical bar: %v", res.Range)
	require.NotNil(t, res.CanonicalBefore)
	assert.True(t, res.CanonicalBefore.Last.Equal(d(2020, 1, 31)))
	require.NotNil(t, res.CanonicalAfter)
	assert.True(t, res.CanonicalAfter.Last.Equal(d(2020, 2, 3)))
}

func TestUpdateDatasets_NoCanonicalData(t *testing.T) {
	s := datasetsService(t, "stooq", d(2020, 3, 1), t.TempDir(), svc.WithArchiveRoot(t.TempDir()))
	resp, err := s.UpdateDatasets(context.Background(), svc.DatasetsRequest{Instruments: symbols("SPY"), Interval: marketdata.D1})
	require.NoError(t, err)
	assert.Equal(t, svc.DatasetFailed, resp.Results[0].Status)
	assert.ErrorIs(t, resp.Results[0].Err, svc.ErrNoCanonicalData)
}

func TestUpdateDatasets_ExplicitRangeBootstrapsStooq(t *testing.T) {
	archiveRoot := t.TempDir()
	writeZIP(t, filepath.Join(archiveRoot, "spy.zip"), map[string]string{"spy.us.txt": spyTwoMonths})
	s := datasetsService(t, "stooq", d(2020, 3, 1), t.TempDir(), svc.WithArchiveRoot(archiveRoot))
	span, err := marketdata.NewTimeRange(d(2020, 1, 1), d(2020, 3, 1))
	require.NoError(t, err)

	resp, err := s.UpdateDatasets(context.Background(), svc.DatasetsRequest{Instruments: symbols("SPY"), Interval: marketdata.D1, Range: span})
	require.NoError(t, err)
	require.NoError(t, resp.Results[0].Err)
	assert.Equal(t, svc.DatasetUpdated, resp.Results[0].Status)
	assert.Nil(t, resp.Results[0].CanonicalBefore)
}

func TestUpdateDatasets_AlreadyAtNow(t *testing.T) {
	ctx := context.Background()
	archiveRoot := t.TempDir()
	writeZIP(t, filepath.Join(archiveRoot, "spy.zip"), map[string]string{"spy.us.txt": spyJanuary})
	// The clock stands at the last canonical bar: nothing can be newer.
	s := datasetsService(t, "stooq", d(2020, 1, 31), t.TempDir(), svc.WithArchiveRoot(archiveRoot))
	req := svc.DatasetsRequest{Instruments: symbols("SPY"), Interval: marketdata.D1}
	_, err := s.CanonicalizeDatasets(ctx, req, false)
	require.NoError(t, err)

	resp, err := s.UpdateDatasets(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, svc.DatasetCurrent, resp.Results[0].Status)
	assert.Zero(t, resp.Results[0].Range, "nothing to act on")
}

func TestUpdateDatasets_RawProvider(t *testing.T) {
	ctx := context.Background()
	s := datasetsService(t, "oanda", d(2024, 2, 1), copyFixtureRaw(t))

	explicit := svc.DatasetsRequest{Instruments: symbols("EURUSD"), Interval: marketdata.H1, Range: fixtureSpan(t)}
	resp, err := s.UpdateDatasets(ctx, explicit)
	require.NoError(t, err)
	require.NoError(t, resp.Results[0].Err, "raw already covers the range: no sync needed")
	assert.Equal(t, svc.DatasetUpdated, resp.Results[0].Status)

	// From the canonical end to now needs raw data past the fixture,
	// which only a sync can fetch; with no OANDA credentials configured
	// that fails, for this symbol only.
	resp, err = s.UpdateDatasets(ctx, svc.DatasetsRequest{Instruments: symbols("EURUSD"), Interval: marketdata.H1})
	require.NoError(t, err)
	assert.Equal(t, svc.DatasetFailed, resp.Results[0].Status)
	assert.Error(t, resp.Results[0].Err)
}

func TestDatasetsCoverage(t *testing.T) {
	ctx := context.Background()
	s := datasetsService(t, "oanda", d(2024, 2, 1), copyFixtureRaw(t))
	req := svc.DatasetsRequest{Instruments: symbols("EURUSD", "BAD"), Interval: marketdata.H1}

	before, err := s.DatasetsCoverage(ctx, req)
	require.NoError(t, err)
	require.Len(t, before.Results, 2)
	require.NoError(t, before.Results[0].Err)
	assert.Empty(t, before.Results[0].Coverage.Partitions, "no canonical data: empty coverage")
	assert.NotNil(t, before.Results[0].Inventory.Raw)
	assert.Error(t, before.Results[1].Err)

	_, err = s.CanonicalizeDatasets(ctx, svc.DatasetsRequest{Instruments: symbols("EURUSD"), Interval: marketdata.H1, Range: fixtureSpan(t)}, false)
	require.NoError(t, err)

	after, err := s.DatasetsCoverage(ctx, req)
	require.NoError(t, err)
	eur := after.Results[0]
	require.NoError(t, eur.Err)
	require.NotNil(t, eur.Inventory.Canonical)
	assert.True(t, eur.Coverage.Range.Start().Equal(eur.Inventory.Canonical.First), "defaults to the canonical span")
	assert.True(t, eur.Coverage.Range.End().Equal(eur.Inventory.Canonical.End))
	assert.NotEmpty(t, eur.Coverage.Partitions)

	explicit := req
	explicit.Range = fixtureSpan(t)
	ranged, err := s.DatasetsCoverage(ctx, explicit)
	require.NoError(t, err)
	assert.True(t, ranged.Results[0].Coverage.Range.Start().Equal(fixtureSpan(t).Start()))
}

func TestDatasetsOperations_CallFailures(t *testing.T) {
	ctx := context.Background()
	req := svc.DatasetsRequest{Instruments: symbols("EURUSD", "USDJPY"), Interval: marketdata.H1}

	t.Run("invalid request", func(t *testing.T) {
		s := datasetsService(t, "oanda", d(2024, 2, 1), copyFixtureRaw(t))
		_, err := s.CanonicalizeDatasets(ctx, svc.DatasetsRequest{Interval: marketdata.H1}, false)
		assert.ErrorIs(t, err, svc.ErrInvalidRequest)
		_, err = s.UpdateDatasets(ctx, svc.DatasetsRequest{Instruments: symbols("EURUSD")})
		assert.ErrorIs(t, err, svc.ErrInvalidRequest)
		_, err = s.DatasetsCoverage(ctx, svc.DatasetsRequest{})
		assert.ErrorIs(t, err, svc.ErrInvalidRequest)
	})
	t.Run("missing resolver", func(t *testing.T) {
		s := newTestService(t)
		_, err := s.CanonicalizeDatasets(ctx, req, false)
		assert.ErrorIs(t, err, svc.ErrResolverNotConfigured)
		_, err = s.DatasetsCoverage(ctx, req)
		assert.ErrorIs(t, err, svc.ErrResolverNotConfigured)
	})
	t.Run("canceled", func(t *testing.T) {
		s := datasetsService(t, "oanda", d(2024, 2, 1), copyFixtureRaw(t))
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := s.UpdateDatasets(canceled, req)
		assert.ErrorIs(t, err, context.Canceled)
		_, err = s.DatasetsCoverage(canceled, req)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestDatasetsOperations_LogOneAggregateRecord(t *testing.T) {
	logger, rec := logging.Capture()
	resolver := instrument.NewMemoryResolver()
	manager, err := marketruntime.New(marketruntime.Config{
		Clock: clock.NewSimulated(d(2024, 2, 1)), StoreRoot: t.TempDir(), RawRoot: copyFixtureRaw(t),
		Resolver: resolver, ProviderName: "oanda",
	})
	require.NoError(t, err)
	s, err := svc.New(manager, logger, svc.WithResolver(resolver))
	require.NoError(t, err)

	_, err = s.CanonicalizeDatasets(context.Background(), svc.DatasetsRequest{Instruments: symbols("EURUSD", "GBPUSD"), Interval: marketdata.H1}, false)
	require.NoError(t, err)

	var aggregate []logging.Record
	for _, r := range rec.Records() {
		if r.Attrs["operation"] == "canonicalize" {
			aggregate = append(aggregate, r)
		}
	}
	require.Len(t, aggregate, 1)
	assert.Equal(t, "datasets canonicalize completed", aggregate[0].Message)
	assert.Equal(t, int64(2), aggregate[0].Attrs["requested"])
	assert.Equal(t, int64(1), aggregate[0].Attrs["failed"])
	assert.Positive(t, aggregate[0].Attrs["published_partitions"])
}

func TestCoverage_OmittedRange(t *testing.T) {
	ctx := context.Background()
	s := datasetsService(t, "oanda", d(2024, 2, 1), copyFixtureRaw(t))
	resolved, err := s.ResolveInstrument(ctx, svc.InstrumentRequest{Symbol: "EURUSD"})
	require.NoError(t, err)
	req := svc.CoverageRequest{DatasetRequest: svc.DatasetRequest{Instrument: resolved.Instrument, Interval: marketdata.H1}}

	empty, err := s.Coverage(ctx, req)
	require.NoError(t, err, "no canonical data is an empty answer, not an error")
	assert.Empty(t, empty.Coverage.Partitions)
	assert.True(t, empty.Coverage.Instrument.Equal(resolved.Instrument))
	assert.True(t, empty.Coverage.Range.Start().IsZero())

	_, err = s.CanonicalizeDatasets(ctx, svc.DatasetsRequest{Instruments: symbols("EURUSD"), Interval: marketdata.H1, Range: fixtureSpan(t)}, false)
	require.NoError(t, err)
	full, err := s.Coverage(ctx, req)
	require.NoError(t, err)
	require.Len(t, full.Coverage.Partitions, 1)
	assert.True(t, full.Coverage.Range.Start().Equal(fixtureSpan(t).Start()))

	_, err = s.Coverage(ctx, svc.CoverageRequest{DatasetRequest: svc.DatasetRequest{Interval: marketdata.H1}})
	assert.ErrorIs(t, err, svc.ErrInvalidRequest, "an omitted range still needs an instrument")
}
