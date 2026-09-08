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

	"github.com/rustyeddy/trader/clock"
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
//
// fakeAlpacaDoer implements http.RoundTripper (issue #323, the Alpaca
// Go SDK migration): the SDK's own ClientOpts.HTTPClient field is a
// concrete *http.Client, not an interface, so this package's test seam
// injects a fake Transport rather than a fake Do-shaped interface —
// the same adjustment marketdata/internal/provider/alpaca's own
// client_test.go makes.

type fakeAlpacaResponse struct {
	status int
	body   string
}

type fakeAlpacaDoer struct {
	mu        sync.Mutex
	responses []fakeAlpacaResponse
	requests  []*http.Request
}

func (f *fakeAlpacaDoer) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return nil, fmt.Errorf("fakeAlpacaDoer: no more responses queued for %s", req.URL)
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	return &http.Response{StatusCode: r.status, Body: io.NopCloser(strings.NewReader(r.body)), Header: make(http.Header)}, nil
}

// alpacaBarsJSONForTest builds a bars response in the shape the
// official Alpaca Go SDK actually decodes (issue #323): "bars" keyed
// by symbol, matching sdkmarketdata's multiBarResponse — not this
// package's own earlier, unverified single-symbol-array assumption.
func alpacaBarsJSONForTest(dates []string) string {
	var b strings.Builder
	b.WriteString(`{"bars":{"SPY":[`)
	for i, d := range dates {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"t":"%sT04:00:00Z","o":100.00,"h":101.00,"l":99.00,"c":100.50,"v":123456}`, d)
	}
	b.WriteString(`]},"next_page_token":null}`)
	return b.String()
}

// newAlpacaTestManagerWithSync returns a Manager wired for Sync: RawRoot
// set, and an *alpaca.Client built with a fake http.RoundTripper (never
// a real network call) injected via Config's own in-package test seam —
// mirroring newTestManagerWithSync (sync_test.go) exactly for the
// "alpaca" provider branch.
func newAlpacaTestManagerWithSync(t *testing.T, rawRoot string, transport http.RoundTripper) *Manager {
	t.Helper()
	client, err := alpaca.NewClient(alpaca.ClientConfig{
		BaseURL:        "https://fake.example.com",
		Credential:     alpaca.StaticCredential{KeyID: "test-key", SecretKey: "test-secret"},
		HTTPClient:     &http.Client{Transport: transport},
		RetryBaseDelay: time.Millisecond,
	})
	require.NoError(t, err)

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
		alpacaClient: client,
	})
	require.NoError(t, err)
	return m
}

// alpacaBarsJSONForTestSymbol is alpacaBarsJSONForTest generalized to
// an arbitrary symbol, needed for
// TestAlpacaSync_FetchesSecondaryInstrumentAAPLThroughSameSDKPath
// (issue #323's own "AAPL can be retrieved through the same path as a
// secondary equity reference instrument" acceptance criterion).
func alpacaBarsJSONForTestSymbol(symbol string, dates []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"bars":{%q:[`, symbol)
	for i, d := range dates {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"t":"%sT04:00:00Z","o":100.00,"h":101.00,"l":99.00,"c":100.50,"v":123456}`, d)
	}
	b.WriteString(`]},"next_page_token":null}`)
	return b.String()
}

// TestAlpacaSync_FetchesSecondaryInstrumentAAPLThroughSameSDKPath
// proves the SDK-delegated fetch path (issue #323) is symbol-agnostic:
// AAPL flows through the identical Sync/FetchBars/recordsFromSDKBars
// path SPY's own sync tests already exercise, not a path that happens
// to work only for the one symbol every other test in this file uses.
func TestAlpacaSync_FetchesSecondaryInstrumentAAPLThroughSameSDKPath(t *testing.T) {
	rawRoot := t.TempDir()
	doer := &fakeAlpacaDoer{responses: []fakeAlpacaResponse{
		{status: 200, body: alpacaBarsJSONForTestSymbol("AAPL", []string{"2020-05-01", "2020-05-04"})},
	}}
	mgr := newAlpacaTestManagerWithSync(t, rawRoot, doer)

	plan := Plan{Actions: []Action{{
		Kind: ActionDownloadRaw, Instrument: alpacaAAPLID(t), Interval: D1,
		Year: 2020, Month: time.May, Reason: "missing",
	}}}
	result, err := mgr.Sync(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Downloaded, 1)
	assert.Equal(t, 2, result.Downloaded[0].RecordsWritten)

	require.Len(t, doer.requests, 1)
	assert.Equal(t, "AAPL", doer.requests[0].URL.Query().Get("symbols"))

	records, err := alpaca.ReadPartitionRecords(context.Background(), rawRoot, "AAPL", 2020, time.May)
	require.NoError(t, err)
	require.Len(t, records, 2)
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

// TestAlpacaSync_NeverRequestsThroughStillFormingTradingDay is PR
// #312 review's third finding: every alpaca.Record is treated as a
// fully closed, final daily bar (syncOneAlpaca's own doc comment), so
// requesting through the current, still-forming trading day would
// risk persisting and canonicalizing a provisional bar as settled
// history. testClock's fixed "now" (2026-01-07T12:00:00Z) is
// 07:00 America/New_York — a real Wednesday, well before the 9:30am
// regular-session open — so the request's own "end" bound must be
// clipped back to midnight UTC of that same day (2026-01-07T00:00:00Z),
// excluding the day itself entirely, rather than sent through "now"
// literally.
func TestAlpacaSync_NeverRequestsThroughStillFormingTradingDay(t *testing.T) {
	rawRoot := t.TempDir()
	doer := &fakeAlpacaDoer{responses: []fakeAlpacaResponse{
		{status: 200, body: alpacaBarsJSONForTest(nil)},
	}}
	mgr := newAlpacaTestManagerWithSync(t, rawRoot, doer)

	plan := Plan{Actions: []Action{{
		Kind: ActionDownloadRaw, Instrument: alpacaSPYID(t), Interval: D1,
		Year: 2026, Month: time.January, Reason: "extend",
	}}}
	_, err := mgr.Sync(context.Background(), plan)
	require.NoError(t, err)

	require.Len(t, doer.requests, 1)
	end := doer.requests[0].URL.Query().Get("end")
	parsed, err := time.Parse(time.RFC3339Nano, end)
	require.NoError(t, err)
	// Alpaca's real "end" parameter is documented as inclusive
	// (sdkmarketdata.GetBarsRequest.End's own doc comment), while
	// BarRequest.To is this codebase's own half-open upper bound
	// (issue #323) — the client converts by subtracting one
	// nanosecond, so the wire value here is one nanosecond before
	// midnight UTC of the still-forming day, not that instant exactly.
	assert.True(t, parsed.Equal(time.Date(2026, time.January, 7, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond)),
		"end = %s, want one nanosecond before midnight UTC of the still-forming day, excluding it", end)
}

// TestAlpacaSync_RequestsThroughTodayOnceSessionHasClosed is the
// control case for TestAlpacaSync_NeverRequestsThroughStillFormingTradingDay:
// once "now" is after the trading day's real regular-session close,
// that day's own bar is legitimately final, and the request's "end"
// bound must include it (not clip back an extra, unnecessary day).
func TestAlpacaSync_RequestsThroughTodayOnceSessionHasClosed(t *testing.T) {
	rawRoot := t.TempDir()
	doer := &fakeAlpacaDoer{responses: []fakeAlpacaResponse{
		{status: 200, body: alpacaBarsJSONForTest(nil)},
	}}
	mgr := newAlpacaTestManagerWithSync(t, rawRoot, doer)
	// 2026-01-07T22:00:00Z = 17:00 America/New_York in January (EST,
	// UTC-5) — one hour after the regular 16:00 close.
	afterClose := clock.NewSimulated(time.Date(2026, time.January, 7, 22, 0, 0, 0, time.UTC))
	mgr.clock = afterClose

	plan := Plan{Actions: []Action{{
		Kind: ActionDownloadRaw, Instrument: alpacaSPYID(t), Interval: D1,
		Year: 2026, Month: time.January, Reason: "extend",
	}}}
	_, err := mgr.Sync(context.Background(), plan)
	require.NoError(t, err)

	require.Len(t, doer.requests, 1)
	end := doer.requests[0].URL.Query().Get("end")
	parsed, err := time.Parse(time.RFC3339Nano, end)
	require.NoError(t, err)
	// See TestAlpacaSync_NeverRequestsThroughStillFormingTradingDay's
	// own comment: "end" is Alpaca's inclusive upper bound, one
	// nanosecond before BarRequest.To's own half-open boundary.
	assert.True(t, parsed.Equal(afterClose.Now().Add(-time.Nanosecond)),
		"end = %s, want one nanosecond before the real current instant (%s) since the session already closed", end, afterClose.Now())
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

// TestMergeAlpacaRecordsByTime_ResultIsSortedByTime confirms
// mergeAlpacaRecordsByTime's output is always ordered by Time,
// regardless of Go's own randomized map-iteration order — the real
// bug PR #312 review found: alpaca.WritePartition deliberately
// preserves whatever order it's given (unlike oanda.WritePartition,
// which sorts internally), so an unsorted merge result would let Sync
// itself write a genuinely out-of-order raw partition. existing and
// fetched are both deliberately supplied out of order here, and
// overlap at one Time (fetched must win that collision).
func TestMergeAlpacaRecordsByTime_ResultIsSortedByTime(t *testing.T) {
	mk := func(day int, closePrice string) alpaca.Record {
		return alpaca.Record{
			Time:  time.Date(2020, time.May, day, 0, 0, 0, 0, time.UTC),
			Close: num.MustParsePrice(closePrice),
		}
	}
	existing := []alpaca.Record{mk(4, "281"), mk(1, "280")}
	fetched := []alpaca.Record{mk(4, "281.5"), mk(6, "283"), mk(5, "282")}

	merged := mergeAlpacaRecordsByTime(existing, fetched)

	require.Len(t, merged, 4)
	for i := 1; i < len(merged); i++ {
		assert.True(t, merged[i-1].Time.Before(merged[i].Time), "merged[%d..%d] out of order: %v, %v", i-1, i, merged[i-1].Time, merged[i].Time)
	}
	assert.True(t, merged[0].Time.Equal(time.Date(2020, time.May, 1, 0, 0, 0, 0, time.UTC)))
	assert.True(t, merged[3].Time.Equal(time.Date(2020, time.May, 6, 0, 0, 0, 0, time.UTC)))
	// fetched's own May 4 record must win the collision with existing's.
	assert.Equal(t, "281.5", merged[1].Close.String())
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
// non-OANDA-provider Manager — but only once a real ActionDownloadRaw
// is actually about to execute (PR #312 review: the original version
// of this test used an empty Plan, which locked in exactly the bug
// the review found — Sync failing even when there is no download work
// to do at all. See TestSync_NoClientRequiredWithoutDownloadActions
// for that corrected behavior.
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
		plan := Plan{Actions: []Action{{
			Kind: ActionDownloadRaw, Instrument: alpacaSPYID(t), Interval: D1,
			Year: 2020, Month: time.May, Reason: "missing",
		}}}
		_, err = mgr.Sync(ctx, plan)
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
		plan := Plan{Actions: []Action{{
			Kind: ActionDownloadRaw, Instrument: spyID(t), Interval: D1,
			Year: 2020, Month: time.May, Reason: "missing",
		}}}
		_, err = mgr.Sync(ctx, plan)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
}

// TestSync_NoClientRequiredWithoutDownloadActions confirms the actual
// bug PR #312 review found is fixed: Sync must not fail merely because
// no provider acquisition client is configured when the plan has
// nothing for one to do — an empty plan, or one containing only
// non-download actions. This matters most for provider "stooq", which
// never has a live acquisition client at all, yet must still be able
// to call Sync with a download-free plan without error.
func TestSync_NoClientRequiredWithoutDownloadActions(t *testing.T) {
	ctx := context.Background()

	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(spyListing(t)))
	mgr, err := New(Config{
		Clock: testClock(), StoreRoot: t.TempDir(), RawRoot: t.TempDir(),
		Resolver: r, ProviderName: "stooq", Calendar: testUSEquityCalendar(),
	})
	require.NoError(t, err)

	_, err = mgr.Sync(ctx, Plan{})
	require.NoError(t, err)

	plan := Plan{Actions: []Action{{
		Kind: ActionNormalizeCanonical, Instrument: spyID(t), Interval: D1,
		Year: 2020, Month: time.May, Reason: "build",
	}}}
	result, err := mgr.Sync(ctx, plan)
	require.NoError(t, err)
	require.Len(t, result.Skipped, 1)
}
