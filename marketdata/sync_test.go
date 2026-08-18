package marketdata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fake OANDA HTTP transport for Sync tests ---

type fakeOandaResponse struct {
	status int
	body   string
}

type fakeOandaDoer struct {
	mu        sync.Mutex
	responses []fakeOandaResponse
	requests  []*http.Request
}

func (f *fakeOandaDoer) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return nil, fmt.Errorf("fakeOandaDoer: no more responses queued for %s", req.URL)
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	return &http.Response{StatusCode: r.status, Body: io.NopCloser(strings.NewReader(r.body))}, nil
}

func (f *fakeOandaDoer) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func candlesJSONForTest(times []time.Time, complete bool) string {
	var b strings.Builder
	b.WriteString(`{"candles":[`)
	for i, t := range times {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"complete":%t,"time":%q,"volume":100,`+
			`"bid":{"o":"1.10000","h":"1.10100","l":"1.09900","c":"1.10050"},`+
			`"ask":{"o":"1.10010","h":"1.10110","l":"1.09910","c":"1.10060"}}`,
			complete, t.Format(time.RFC3339Nano))
	}
	b.WriteString(`]}`)
	return b.String()
}

// newTestManagerWithSync returns a Manager wired for Sync: RawRoot set,
// and an *oanda.Client built with a fake HTTPDoer (never a real network
// call) injected via Config's own in-package test seam.
func newTestManagerWithSync(t *testing.T, rawRoot string, doer oanda.HTTPDoer) *Manager {
	t.Helper()
	client, err := oanda.NewClient(oanda.ClientConfig{
		BaseURL:        "https://fake.example.com",
		Credential:     oanda.StaticCredential("test-token"),
		HTTPClient:     doer,
		RetryBaseDelay: time.Millisecond,
	})
	require.NoError(t, err)

	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     testResolver(t),
		ProviderName: "oanda",
		oandaClient:  client,
	})
	require.NoError(t, err)
	return m
}

func downloadAction(year int, month time.Month) Action {
	return Action{
		Kind: ActionDownloadRaw, Instrument: eurusd(), Interval: H1,
		Year: year, Month: month, Reason: "missing",
	}
}

func TestSync_DownloadsMissingRawPartition(t *testing.T) {
	rawRoot := t.TempDir()
	doer := &fakeOandaDoer{responses: []fakeOandaResponse{
		{status: 200, body: candlesJSONForTest([]time.Time{
			time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 3, 2, 1, 0, 0, 0, time.UTC),
		}, true)},
	}}
	mgr := newTestManagerWithSync(t, rawRoot, doer)

	plan := Plan{Actions: []Action{downloadAction(2020, time.March)}}
	result, err := mgr.Sync(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Downloaded, 1)
	assert.Equal(t, 2, result.Downloaded[0].RecordsWritten)
	assert.Empty(t, result.Skipped)

	records, err := oanda.ReadPartitionRecords(context.Background(), rawRoot, "EURUSD", oanda.RawH1, 2020, time.March)
	require.NoError(t, err)
	require.Len(t, records, 2)
}

func TestSync_ExtendsExistingRawPartition(t *testing.T) {
	rawRoot := t.TempDir()
	existing := []oanda.Record{{
		Time:    time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC),
		BidOpen: num.MustParsePrice("1.1"), BidHigh: num.MustParsePrice("1.1"), BidLow: num.MustParsePrice("1.1"), BidClose: num.MustParsePrice("1.1"),
		AskOpen: num.MustParsePrice("1.1"), AskHigh: num.MustParsePrice("1.1"), AskLow: num.MustParsePrice("1.1"), AskClose: num.MustParsePrice("1.1"),
		Volume: 10, Complete: true,
	}}
	require.NoError(t, oanda.WritePartition(context.Background(), rawRoot, "EURUSD", oanda.RawH1, 2020, time.March, existing, true))

	doer := &fakeOandaDoer{responses: []fakeOandaResponse{
		{status: 200, body: candlesJSONForTest([]time.Time{time.Date(2020, 3, 2, 1, 0, 0, 0, time.UTC)}, true)},
	}}
	mgr := newTestManagerWithSync(t, rawRoot, doer)

	plan := Plan{Actions: []Action{{
		Kind: ActionDownloadRaw, Instrument: eurusd(), Interval: H1,
		Year: 2020, Month: time.March, Reason: "extend",
	}}}
	result, err := mgr.Sync(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Downloaded, 1)
	assert.Equal(t, 2, result.Downloaded[0].RecordsWritten)

	// The fetch must have started after the existing record, not
	// bulk-redownloaded the whole month.
	gotFrom := doer.requests[0].URL.Query().Get("from")
	wantFrom := existing[0].Time.Add(time.Nanosecond).Format(time.RFC3339Nano)
	assert.Equal(t, wantFrom, gotFrom)

	records, err := oanda.ReadPartitionRecords(context.Background(), rawRoot, "EURUSD", oanda.RawH1, 2020, time.March)
	require.NoError(t, err)
	require.Len(t, records, 2)
}

func TestSync_SkipsNonDownloadActions(t *testing.T) {
	rawRoot := t.TempDir()
	doer := &fakeOandaDoer{}
	mgr := newTestManagerWithSync(t, rawRoot, doer)

	plan := Plan{Actions: []Action{
		{Kind: ActionNormalizeCanonical, Instrument: eurusd(), Interval: H1, Year: 2020, Month: time.March},
		{Kind: ActionDeriveCanonical, Instrument: eurusd(), Interval: W1, Year: 2020, Month: time.March},
	}}
	result, err := mgr.Sync(context.Background(), plan)
	require.NoError(t, err)
	assert.Empty(t, result.Downloaded)
	require.Len(t, result.Skipped, 2)
	assert.Equal(t, 0, doer.requestCount(), "non-download actions must never trigger a request")
}

func TestSync_RequiresOANDAClientConfigured(t *testing.T) {
	mgr := newTestManager(t) // no OANDA client wired (query_test.go's helper)
	_, err := mgr.Sync(context.Background(), Plan{Actions: []Action{downloadAction(2020, time.March)}})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestSync_RequiresRawRoot(t *testing.T) {
	mgr := newTestManagerWithSync(t, "", &fakeOandaDoer{})
	_, err := mgr.Sync(context.Background(), Plan{Actions: []Action{downloadAction(2020, time.March)}})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestSync_NotConfigured(t *testing.T) {
	var mgr Manager
	_, err := mgr.Sync(context.Background(), Plan{})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestSync_CancelledMidLoop(t *testing.T) {
	rawRoot := t.TempDir()
	doer := &fakeOandaDoer{responses: []fakeOandaResponse{
		{status: 200, body: candlesJSONForTest([]time.Time{time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)}, true)},
	}}
	mgr := newTestManagerWithSync(t, rawRoot, doer)

	ctx := &nthCancelContext{Context: context.Background(), remaining: 1}
	plan := Plan{Actions: []Action{
		downloadAction(2020, time.March),
		downloadAction(2020, time.April),
	}}
	_, err := mgr.Sync(ctx, plan)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSync_PropagatesFetchError(t *testing.T) {
	rawRoot := t.TempDir()
	doer := &fakeOandaDoer{responses: []fakeOandaResponse{
		{status: 500, body: "err"}, {status: 500, body: "err"}, {status: 500, body: "err"},
	}}
	mgr := newTestManagerWithSync(t, rawRoot, doer)

	_, err := mgr.Sync(context.Background(), Plan{Actions: []Action{downloadAction(2020, time.March)}})
	require.Error(t, err)
	assert.ErrorIs(t, err, oanda.ErrRetriesExhausted)
	assert.Contains(t, err.Error(), "2020-03")
}

func TestSync_FutureMonthIsNoOp(t *testing.T) {
	rawRoot := t.TempDir()
	doer := &fakeOandaDoer{}
	mgr := newTestManagerWithSync(t, rawRoot, doer)

	// testClock's "now" is 2026-01-07; a February 2026 action is
	// entirely in the future relative to it.
	plan := Plan{Actions: []Action{downloadAction(2026, time.February)}}
	result, err := mgr.Sync(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Downloaded, 1)
	assert.Equal(t, 0, result.Downloaded[0].RecordsWritten)
	assert.Equal(t, 0, doer.requestCount(), "nothing to fetch yet; must not call out")

	_, err = oanda.ReadPartitionRecords(context.Background(), rawRoot, "EURUSD", oanda.RawH1, 2026, time.February)
	assert.Error(t, err, "no file should have been written")
}

func TestSync_UnsupportedRawInterval(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithSync(t, rawRoot, &fakeOandaDoer{})
	odd, err := NewInterval(UnitHour, 7)
	require.NoError(t, err)

	_, err = mgr.Sync(context.Background(), Plan{Actions: []Action{{
		Kind: ActionDownloadRaw, Instrument: eurusd(), Interval: odd, Year: 2020, Month: time.March,
	}}})
	assert.Error(t, err)
}
