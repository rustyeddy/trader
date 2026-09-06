package alpaca

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fake HTTP transport: no test in this file makes a real network call ---
// Mirrors oanda/client_test.go's identical fakeDoer/cancelAfterDoer shape.

type fakeResponse struct {
	status int
	body   string
	err    error
}

type fakeDoer struct {
	mu        sync.Mutex
	responses []fakeResponse
	requests  []*http.Request
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return nil, fmt.Errorf("fakeDoer: no more responses queued for %s", req.URL)
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{StatusCode: r.status, Body: io.NopCloser(strings.NewReader(r.body))}, nil
}

func (f *fakeDoer) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeDoer) lastRequest() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

type cancelAfterDoer struct {
	inner  *fakeDoer
	cancel context.CancelFunc
	afterN int
	calls  int
}

func (d *cancelAfterDoer) Do(req *http.Request) (*http.Response, error) {
	d.calls++
	resp, err := d.inner.Do(req)
	if d.calls == d.afterN {
		d.cancel()
	}
	return resp, err
}

// barsJSON builds an assumed-shape bars response body for the given
// (date, close) pairs, all sharing fixed O/H/L values and a fixed
// volume — sufficient for pagination/parsing tests, which only care
// about Time/Close/count. Each date is rendered as a session-open
// instant in UTC (05:00, i.e. midnight America/New_York in winter) —
// deliberately not midnight UTC — so tests exercise the real
// timestamp-normalization path (wireshape.go), not a coincidentally
// pre-aligned fixture.
func barsJSON(dates []string, nextPageToken string) string {
	var b strings.Builder
	b.WriteString(`{"symbol":"SPY","bars":[`)
	for i, d := range dates {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"t":"%sT05:00:00Z","o":100.00,"h":101.00,"l":99.00,"c":100.50,"v":123456}`, d)
	}
	b.WriteString(`]`)
	if nextPageToken != "" {
		fmt.Fprintf(&b, `,"next_page_token":%q`, nextPageToken)
	} else {
		b.WriteString(`,"next_page_token":null`)
	}
	b.WriteString(`}`)
	return b.String()
}

const (
	testKeyID     = "test-key-id-abc"
	testSecretKey = "test-secret-key-xyz"
)

func newTestClient(t *testing.T, doer HTTPDoer, opts ...func(*ClientConfig)) *Client {
	t.Helper()
	cfg := ClientConfig{
		BaseURL:        "https://fake.example.com",
		Credential:     StaticCredential{KeyID: testKeyID, SecretKey: testSecretKey},
		RetryBaseDelay: time.Millisecond,
		HTTPClient:     doer,
		clock:          clock.Real{},
	}
	for _, o := range opts {
		o(&cfg)
	}
	c, err := NewClient(cfg)
	require.NoError(t, err)
	return c
}

var (
	testFrom = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testTo   = time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
)

func TestFetchBars_SinglePage(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: barsJSON([]string{"2024-01-02", "2024-01-03"}, "")},
	}}
	c := newTestClient(t, doer)

	records, err := c.FetchBars(context.Background(), BarRequest{Symbol: "spy", From: testFrom, To: testTo})
	require.NoError(t, err)
	require.Len(t, records, 2)

	// Time is re-anchored to midnight UTC of the trading date, not the
	// literal "05:00:00Z" fetched instant.
	assert.True(t, records[0].Time.Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, "100.5", records[0].Close.String())
	assert.Equal(t, "100", records[0].Open.String())
	assert.Equal(t, int64(123456), records[0].Volume)

	require.Equal(t, 1, doer.requestCount())
	req := doer.lastRequest()
	assert.Equal(t, "/v2/stocks/SPY/bars", req.URL.Path, "symbol must be uppercased")
	q := req.URL.Query()
	assert.Equal(t, "1Day", q.Get("timeframe"))
	assert.Equal(t, "split", q.Get("adjustment"))
	assert.Equal(t, "iex", q.Get("feed"))
	assert.Equal(t, testKeyID, req.Header.Get("APCA-API-KEY-ID"))
	assert.Equal(t, testSecretKey, req.Header.Get("APCA-API-SECRET-KEY"))
}

func TestFetchBars_EmptyResponse(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 200, body: barsJSON(nil, "")}}}
	c := newTestClient(t, doer)
	records, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestFetchBars_Paginates(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: barsJSON([]string{"2024-01-02"}, "page2token")},
		{status: 200, body: barsJSON([]string{"2024-01-03"}, "")},
	}}
	c := newTestClient(t, doer)

	records, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, 2, doer.requestCount())
	assert.Equal(t, "page2token", doer.requests[1].URL.Query().Get("page_token"))
	assert.Empty(t, doer.requests[0].URL.Query().Get("page_token"))
}

func TestFetchBars_RetriesTransientThenSucceeds(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{err: errors.New("connection reset")},
		{status: 503, body: "service unavailable"},
		{status: 200, body: barsJSON([]string{"2024-01-02"}, "")},
	}}
	c := newTestClient(t, doer)
	records, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, 3, doer.requestCount())
}

func TestFetchBars_RetriesExhausted(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 429, body: "rate limited"},
		{status: 429, body: "rate limited"},
		{status: 429, body: "rate limited"},
	}}
	c := newTestClient(t, doer, func(cfg *ClientConfig) { cfg.MaxAttempts = 3 })
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRetriesExhausted)
	assert.Equal(t, 3, doer.requestCount())
}

func TestFetchBars_PermanentErrorNotRetried(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"unauthorized", http.StatusUnauthorized, ErrUnauthorized},
		{"forbidden", http.StatusForbidden, ErrUnauthorized},
		{"bad request", http.StatusBadRequest, ErrBadRequest},
		{"not found", http.StatusNotFound, ErrBadRequest},
		{"teapot", http.StatusTeapot, ErrUnexpectedStatus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doer := &fakeDoer{responses: []fakeResponse{{status: tc.status, body: "denied"}}}
			c := newTestClient(t, doer)
			_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.want)
			assert.Equal(t, 1, doer.requestCount(), "a permanent error must not be retried")
		})
	}
}

func TestFetchBars_MalformedJSONIsBadRequest(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 200, body: "{not json"}}}
	c := newTestClient(t, doer)
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	assert.ErrorIs(t, err, ErrBadRequest)
}

func TestFetchBars_CredentialsNeverAppearInError(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 401, body: "invalid credentials"}}}
	c := newTestClient(t, doer)
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), testKeyID)
	assert.NotContains(t, err.Error(), testSecretKey)
}

func TestFetchBars_RejectsEmptySymbol(t *testing.T) {
	c := newTestClient(t, &fakeDoer{})
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "  ", From: testFrom, To: testTo})
	assert.ErrorIs(t, err, ErrBadRequest)
}

func TestFetchBars_RejectsInvalidRange(t *testing.T) {
	c := newTestClient(t, &fakeDoer{})
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testTo, To: testFrom})
	assert.ErrorIs(t, err, ErrBadRequest)
}

func TestFetchBars_AlreadyCancelledContext(t *testing.T) {
	c := newTestClient(t, &fakeDoer{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.FetchBars(ctx, BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFetchBars_CancelledMidPagination(t *testing.T) {
	inner := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: barsJSON([]string{"2024-01-02"}, "page2token")},
		{status: 200, body: barsJSON([]string{"2024-01-03"}, "")},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	doer := &cancelAfterDoer{inner: inner, cancel: cancel, afterN: 1}
	c := newTestClient(t, doer)

	_, err := c.FetchBars(ctx, BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, inner.requestCount(), "must not issue the second page's request after cancellation")
}

func TestNewClient_RequiresBaseURLAndCredential(t *testing.T) {
	_, err := NewClient(ClientConfig{Credential: StaticCredential{KeyID: "k", SecretKey: "s"}})
	assert.Error(t, err)
	_, err = NewClient(ClientConfig{BaseURL: "https://fake.example.com"})
	assert.Error(t, err)
}

func TestStaticCredential(t *testing.T) {
	keyID, secretKey, err := StaticCredential{KeyID: "k", SecretKey: "s"}.Credentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "k", keyID)
	assert.Equal(t, "s", secretKey)
}
