package marketdata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata/internal/provider/alpaca"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alpacaSPYListing/alpacaSPYID mirror spyListing/spyID (stooq_integration_test.go)
// but under provider "alpaca" — Alpaca's own bare-ticker native symbol
// ("SPY", no venue suffix), matching EquityRegistration.ProviderSymbol's
// documented convention (service/marketdata, EQ-02).
func alpacaSPYListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "alpaca",
		Venue:      "ARCA",
		Symbol:     "SPY",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func alpacaSPYID(t *testing.T) instrument.ID {
	t.Helper()
	inst, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	return inst.ID()
}

func alpacaAAPLListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "alpaca",
		Venue:      "NASDAQ",
		Symbol:     "AAPL",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func alpacaAAPLID(t *testing.T) instrument.ID {
	t.Helper()
	inst, err := instrument.NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	return inst.ID()
}

// newAlpacaTestManager returns a Manager rooted at t.TempDir(), wired
// with a resolver holding both alpacaSPYListing and alpacaAAPLListing,
// provider "alpaca", a *USEquityCalendar (the same Calendar Stooq's own
// end-to-end tests use, since both providers validate D1 alignment
// against the identical midnight-UTC boundary), and rawRoot pointing at
// the given directory.
func newAlpacaTestManager(t *testing.T, rawRoot string) *Manager {
	t.Helper()
	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(alpacaSPYListing(t)))
	require.NoError(t, r.Register(alpacaAAPLListing(t)))
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     r,
		ProviderName: "alpaca",
		Calendar:     testUSEquityCalendar(),
	})
	require.NoError(t, err)
	return m
}

// TestAlpacaEndToEnd_RejectsWrongCalendarType mirrors
// TestStooqEndToEnd_RejectsWrongCalendarType exactly, for the "alpaca"
// provider branch readAndNormalizeRaw's own case added.
func TestAlpacaEndToEnd_RejectsWrongCalendarType(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	rec := alpaca.Record{
		Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		Open: num.MustParsePrice("282.80"), High: num.MustParsePrice("283.19"),
		Low: num.MustParsePrice("278.85"), Close: num.MustParsePrice("282.79"), Volume: 74424000,
	}
	require.NoError(t, alpaca.WritePartition(ctx, rawRoot, "SPY", 2020, time.May, []alpaca.Record{rec}, true))

	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(alpacaSPYListing(t)))
	mgr, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     resolver,
		ProviderName: "alpaca",
		// Deliberately not a *USEquityCalendar: the default FXCalendar,
		// wrong for this provider.
	})
	require.NoError(t, err)

	span, err := NewTimeRange(
		time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: alpacaSPYID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)

	_, err = mgr.Build(ctx, plan)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

// TestAlpacaEndToEnd_PlanBuildBars is EQ-04's central acceptance
// criterion: SPY raw bars flow through the exact same Plan -> Build ->
// Bars path FX/Stooq data uses, with marketdata.Manager's public
// surface never referencing anything Alpaca-specific.
func TestAlpacaEndToEnd_PlanBuildBars(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	records := []alpaca.Record{
		{Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
			Open: num.MustParsePrice("282.80"), High: num.MustParsePrice("283.19"),
			Low: num.MustParsePrice("278.85"), Close: num.MustParsePrice("282.79"), Volume: 74424000},
		{Time: time.Date(2020, 5, 4, 0, 0, 0, 0, time.UTC),
			Open: num.MustParsePrice("280.34"), High: num.MustParsePrice("286.44"),
			Low: num.MustParsePrice("278.83"), Close: num.MustParsePrice("285.34"), Volume: 61139700},
	}
	require.NoError(t, alpaca.WritePartition(ctx, rawRoot, "SPY", 2020, time.May, records, true))

	mgr := newAlpacaTestManager(t, rawRoot)

	span, err := NewTimeRange(
		time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: alpacaSPYID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)
	for _, a := range plan.Actions {
		assert.NotEqual(t, ActionDownloadRaw, a.Kind, "unexpected download action: %+v", a)
	}

	buildResult, err := mgr.Build(ctx, plan)
	require.NoError(t, err)
	require.NotEmpty(t, buildResult.Published)
	for _, pr := range buildResult.Published {
		assert.Equal(t, BasisTrade, pr.Manifest.Basis)
		assert.Equal(t, AdjustmentSplitAdjusted, pr.Manifest.AdjustmentPolicy)
		assert.Equal(t, calendarVersionUSEquityV1, pr.Manifest.CalendarVersion)
		assert.Equal(t, "alpaca", pr.Manifest.Provider)
	}

	reader, err := mgr.Bars(ctx, query)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	var bars []Bar
	for {
		b, err := reader.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		bars = append(bars, b)
	}

	require.Len(t, bars, 2)
	assert.True(t, bars[0].Time.Equal(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, "282.8", bars[0].Open.String())
	assert.Equal(t, "283.19", bars[0].High.String())
	assert.Equal(t, "278.85", bars[0].Low.String())
	assert.Equal(t, "282.79", bars[0].Close.String())
	assert.Equal(t, int64(74424000), bars[0].Ticks)
	assert.True(t, bars[0].AvgSpread.IsZero())
	assert.True(t, bars[0].MaxSpread.IsZero())
	assert.True(t, bars[1].Time.Equal(time.Date(2020, 5, 4, 0, 0, 0, 0, time.UTC)))
}

// TestAlpacaEndToEnd_AAPLPlanBuildBars is the secondary-instrument
// acceptance criterion issue #297 asks for, mirroring
// TestAlpacaEndToEnd_PlanBuildBars for AAPL under the same Manager.
func TestAlpacaEndToEnd_AAPLPlanBuildBars(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	records := []alpaca.Record{
		{Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
			Open: num.MustParsePrice("286.25"), High: num.MustParsePrice("299.00"),
			Low: num.MustParsePrice("285.00"), Close: num.MustParsePrice("297.56"), Volume: 45765000},
	}
	require.NoError(t, alpaca.WritePartition(ctx, rawRoot, "AAPL", 2020, time.May, records, true))

	mgr := newAlpacaTestManager(t, rawRoot)
	span, err := NewTimeRange(
		time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: alpacaAAPLID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)
	buildResult, err := mgr.Build(ctx, plan)
	require.NoError(t, err)
	require.NotEmpty(t, buildResult.Published)

	reader, err := mgr.Bars(ctx, query)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	b, err := reader.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, "297.56", b.Close.String())
}

// TestAlpacaEndToEnd_RejectsCorruptRawData mirrors
// TestStooqEndToEnd_RejectsCorruptRawData for Alpaca's own
// normalizeAlpacaSequence/Bar.Validate reuse.
func TestAlpacaEndToEnd_RejectsCorruptRawData(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	rec := alpaca.Record{
		Time:  time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		Open:  num.MustParsePrice("100"),
		High:  num.MustParsePrice("90"), // High < Low: invalid OHLC
		Low:   num.MustParsePrice("95"),
		Close: num.MustParsePrice("92"),
	}
	require.NoError(t, alpaca.WritePartition(ctx, rawRoot, "SPY", 2020, time.May, []alpaca.Record{rec}, true))

	mgr := newAlpacaTestManager(t, rawRoot)
	span, err := NewTimeRange(
		time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: alpacaSPYID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)

	_, err = mgr.Build(ctx, plan)
	assert.Error(t, err)

	_, err = mgr.Bars(ctx, query)
	assert.ErrorIs(t, err, ErrDataUnavailable)
}

// TestAlpacaEndToEnd_RejectsOutOfOrderRawData mirrors
// TestStooqEndToEnd_RejectsOutOfOrderRawData.
func TestAlpacaEndToEnd_RejectsOutOfOrderRawData(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	later := alpaca.Record{
		Time: time.Date(2020, 5, 4, 0, 0, 0, 0, time.UTC),
		Open: num.MustParsePrice("280.34"), High: num.MustParsePrice("286.44"),
		Low: num.MustParsePrice("278.83"), Close: num.MustParsePrice("285.34"),
	}
	earlier := alpaca.Record{
		Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		Open: num.MustParsePrice("282.80"), High: num.MustParsePrice("283.19"),
		Low: num.MustParsePrice("278.85"), Close: num.MustParsePrice("282.79"),
	}
	require.NoError(t, alpaca.WritePartition(ctx, rawRoot, "SPY", 2020, time.May,
		[]alpaca.Record{later, earlier}, true))

	mgr := newAlpacaTestManager(t, rawRoot)
	span, err := NewTimeRange(
		time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: alpacaSPYID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)

	_, err = mgr.Build(ctx, plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), errRecordOutOfOrder.Error())

	_, err = mgr.Bars(ctx, query)
	assert.ErrorIs(t, err, ErrDataUnavailable)
}

// --- Sync (fake Alpaca HTTP transport) ---

type fakeAlpacaResponse struct {
	status int
	body   string
}

type fakeAlpacaDoer struct {
	mu        sync.Mutex
	responses []fakeAlpacaResponse
	requests  []*http.Request
}

func (f *fakeAlpacaDoer) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return nil, fmt.Errorf("fakeAlpacaDoer: no more responses queued for %s", req.URL)
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	return &http.Response{StatusCode: r.status, Body: io.NopCloser(strings.NewReader(r.body))}, nil
}

func alpacaBarsJSONForTest(dates []string) string {
	var b strings.Builder
	b.WriteString(`{"symbol":"SPY","bars":[`)
	for i, d := range dates {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"t":"%sT04:00:00Z","o":100.00,"h":101.00,"l":99.00,"c":100.50,"v":123456}`, d)
	}
	b.WriteString(`],"next_page_token":null}`)
	return b.String()
}

// newAlpacaTestManagerWithSync returns a Manager wired for Sync: RawRoot
// set, and an *alpaca.Client built with a fake HTTPDoer (never a real
// network call) injected via Config's own in-package test seam —
// mirroring newTestManagerWithSync (sync_test.go) exactly for the
// "alpaca" provider branch.
func newAlpacaTestManagerWithSync(t *testing.T, rawRoot string, doer alpaca.HTTPDoer) *Manager {
	t.Helper()
	client, err := alpaca.NewClient(alpaca.ClientConfig{
		BaseURL:        "https://fake.example.com",
		Credential:     alpaca.StaticCredential{KeyID: "test-key", SecretKey: "test-secret"},
		HTTPClient:     doer,
		RetryBaseDelay: time.Millisecond,
	})
	require.NoError(t, err)

	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(alpacaSPYListing(t)))

	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     r,
		ProviderName: "alpaca",
		Calendar:     testUSEquityCalendar(),
		alpacaClient: client,
	})
	require.NoError(t, err)
	return m
}

func TestAlpacaSync_DownloadsMissingRawPartition(t *testing.T) {
	rawRoot := t.TempDir()
	doer := &fakeAlpacaDoer{responses: []fakeAlpacaResponse{
		{status: 200, body: alpacaBarsJSONForTest([]string{"2020-05-01", "2020-05-04"})},
	}}
	mgr := newAlpacaTestManagerWithSync(t, rawRoot, doer)

	plan := Plan{Actions: []Action{{
		Kind: ActionDownloadRaw, Instrument: alpacaSPYID(t), Interval: D1,
		Year: 2020, Month: time.May, Reason: "missing",
	}}}
	result, err := mgr.Sync(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Downloaded, 1)
	assert.Equal(t, 2, result.Downloaded[0].RecordsWritten)
	assert.Empty(t, result.Skipped)

	records, err := alpaca.ReadPartitionRecords(context.Background(), rawRoot, "SPY", 2020, time.May)
	require.NoError(t, err)
	require.Len(t, records, 2)
}

func TestAlpacaSync_ExtendsExistingRawPartition(t *testing.T) {
	rawRoot := t.TempDir()
	existing := []alpaca.Record{{
		Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		Open: num.MustParsePrice("100"), High: num.MustParsePrice("101"),
		Low: num.MustParsePrice("99"), Close: num.MustParsePrice("100.5"), Volume: 1000,
	}}
	require.NoError(t, alpaca.WritePartition(context.Background(), rawRoot, "SPY", 2020, time.May, existing, true))

	doer := &fakeAlpacaDoer{responses: []fakeAlpacaResponse{
		{status: 200, body: alpacaBarsJSONForTest([]string{"2020-05-04"})},
	}}
	mgr := newAlpacaTestManagerWithSync(t, rawRoot, doer)

	plan := Plan{Actions: []Action{{
		Kind: ActionDownloadRaw, Instrument: alpacaSPYID(t), Interval: D1,
		Year: 2020, Month: time.May, Reason: "extend",
	}}}
	result, err := mgr.Sync(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Downloaded, 1)
	assert.Equal(t, 2, result.Downloaded[0].RecordsWritten)

	records, err := alpaca.ReadPartitionRecords(context.Background(), rawRoot, "SPY", 2020, time.May)
	require.NoError(t, err)
	require.Len(t, records, 2)
}

func TestAlpacaSync_NonAlpacaDownloadActionsAreSkipped(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newAlpacaTestManagerWithSync(t, rawRoot, &fakeAlpacaDoer{})

	plan := Plan{Actions: []Action{{
		Kind: ActionNormalizeCanonical, Instrument: alpacaSPYID(t), Interval: D1,
		Year: 2020, Month: time.May, Reason: "build",
	}}}
	result, err := mgr.Sync(context.Background(), plan)
	require.NoError(t, err)
	assert.Empty(t, result.Downloaded)
	require.Len(t, result.Skipped, 1)
}

// TestSync_RequiresMatchingProviderClient confirms Sync's generalized
// requireSyncClient check (sync.go) reports a clear ErrInvalidConfig
// for each provider missing its own client — never a nil-pointer
// dereference, and never a misleading "OANDA" message for a
// non-OANDA-provider Manager.
func TestSync_RequiresMatchingProviderClient(t *testing.T) {
	ctx := context.Background()

	t.Run("alpaca without alpacaClient", func(t *testing.T) {
		r := instrument.NewMemoryResolver()
		require.NoError(t, r.Register(alpacaSPYListing(t)))
		mgr, err := New(Config{
			Clock: testClock(), StoreRoot: t.TempDir(), RawRoot: t.TempDir(),
			Resolver: r, ProviderName: "alpaca", Calendar: testUSEquityCalendar(),
		})
		require.NoError(t, err)
		_, err = mgr.Sync(ctx, Plan{})
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})

	t.Run("stooq has no live acquisition client", func(t *testing.T) {
		r := instrument.NewMemoryResolver()
		require.NoError(t, r.Register(spyListing(t)))
		mgr, err := New(Config{
			Clock: testClock(), StoreRoot: t.TempDir(), RawRoot: t.TempDir(),
			Resolver: r, ProviderName: "stooq", Calendar: testUSEquityCalendar(),
		})
		require.NoError(t, err)
		_, err = mgr.Sync(ctx, Plan{})
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
}
