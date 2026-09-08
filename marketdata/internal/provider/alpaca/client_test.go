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
//
// The underlying SDK (github.com/alpacahq/alpaca-trade-api-go/v3,
// issue #323) accepts a concrete *http.Client, not an interface, so
// this package's test seam is a fake http.RoundTripper injected as
// that *http.Client's Transport — the standard Go idiom for faking an
// HTTP client — rather than the previous fakeDoer-implements-HTTPDoer
// shape (removed along with HTTPDoer itself).

type fakeResponse struct {
	status int
	body   string
	err    error
}

type fakeTransport struct {
	mu        sync.Mutex
	responses []fakeResponse
	requests  []*http.Request
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return nil, fmt.Errorf("fakeTransport: no more responses queued for %s", req.URL)
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{StatusCode: r.status, Body: io.NopCloser(strings.NewReader(r.body)), Header: make(http.Header)}, nil
}

func (f *fakeTransport) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeTransport) lastRequest() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

// barsJSON builds a bars response in the shape the official Alpaca Go
// SDK actually decodes (issue #323): "bars" keyed by symbol
// (sdkmarketdata's own multiBarResponse, confirmed by reading the
// SDK's source directly — GetBars/GetMultiBars always call the
// multi-symbol /v2/stocks/bars?symbols=... endpoint, never a
// single-symbol path endpoint), not this package's own earlier,
// unverified single-symbol-array assumption.
func barsJSON(symbol string, dates []string, nextPageToken string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"bars":{%q:[`, symbol)
	for i, d := range dates {
		if i > 0 {
			b.WriteString(",")
		}
		// Rendered at midnight America/New_York, expressed in UTC
		// (05:00 in winter/EST) — not the 09:30 regular-session open,
		// and deliberately not midnight UTC either — so tests exercise
		// the real timestamp-normalization path (wireshape.go), not a
		// coincidentally pre-aligned fixture.
		fmt.Fprintf(&b, `{"t":"%sT05:00:00Z","o":100.00,"h":101.00,"l":99.00,"c":100.50,"v":123456}`, d)
	}
	b.WriteString(`]}`)
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

func newTestClient(t *testing.T, transport http.RoundTripper, opts ...func(*ClientConfig)) *Client {
	t.Helper()
	cfg := ClientConfig{
		BaseURL:        "https://fake.example.com",
		Credential:     StaticCredential{KeyID: testKeyID, SecretKey: testSecretKey},
		RetryBaseDelay: time.Millisecond,
		HTTPClient:     &http.Client{Transport: transport},
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
	transport := &fakeTransport{responses: []fakeResponse{
		{status: 200, body: barsJSON("SPY", []string{"2024-01-02", "2024-01-03"}, "")},
	}}
	c := newTestClient(t, transport)

	records, err := c.FetchBars(context.Background(), BarRequest{Symbol: "spy", From: testFrom, To: testTo})
	require.NoError(t, err)
	require.Len(t, records, 2)

	// Time is re-anchored to midnight UTC of the trading date, not the
	// literal "05:00:00Z" fetched instant.
	assert.True(t, records[0].Time.Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, "100.5", records[0].Close.String())
	assert.Equal(t, "100", records[0].Open.String())
	assert.Equal(t, int64(123456), records[0].Volume)

	require.Equal(t, 1, transport.requestCount())
	req := transport.lastRequest()
	assert.Equal(t, "/v2/stocks/bars", req.URL.Path)
	q := req.URL.Query()
	assert.Equal(t, "SPY", q.Get("symbols"), "symbol must be uppercased")
	assert.Equal(t, "1Day", q.Get("timeframe"))
	assert.Equal(t, "split", q.Get("adjustment"))
	assert.Equal(t, "iex", q.Get("feed"))
	assert.Equal(t, testKeyID, req.Header.Get("APCA-API-KEY-ID"))
	assert.Equal(t, testSecretKey, req.Header.Get("APCA-API-SECRET-KEY"))
}

func TestFetchBars_ExplicitFeedOverridesDefault(t *testing.T) {
	transport := &fakeTransport{responses: []fakeResponse{
		{status: 200, body: barsJSON("SPY", nil, "")},
	}}
	c := newTestClient(t, transport, func(cfg *ClientConfig) { cfg.Feed = FeedSIP })
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.NoError(t, err)
	assert.Equal(t, "sip", transport.lastRequest().URL.Query().Get("feed"))
}

func TestFetchBars_EmptyResponse(t *testing.T) {
	transport := &fakeTransport{responses: []fakeResponse{{status: 200, body: barsJSON("SPY", nil, "")}}}
	c := newTestClient(t, transport)
	records, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestFetchBars_Paginates(t *testing.T) {
	transport := &fakeTransport{responses: []fakeResponse{
		{status: 200, body: barsJSON("SPY", []string{"2024-01-02"}, "page2token")},
		{status: 200, body: barsJSON("SPY", []string{"2024-01-03"}, "")},
	}}
	c := newTestClient(t, transport)

	records, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, 2, transport.requestCount())
	assert.Equal(t, "page2token", transport.requests[1].URL.Query().Get("page_token"))
	assert.Empty(t, transport.requests[0].URL.Query().Get("page_token"))
}

func TestFetchBars_RetriesTransientThenSucceeds(t *testing.T) {
	transport := &fakeTransport{responses: []fakeResponse{
		{err: errors.New("connection reset")},
		{status: 503, body: `{"message":"service unavailable","code":50310000}`},
		{status: 200, body: barsJSON("SPY", []string{"2024-01-02"}, "")},
	}}
	c := newTestClient(t, transport)
	records, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, 3, transport.requestCount())
}

func TestFetchBars_RetriesExhausted(t *testing.T) {
	rateLimited := fakeResponse{status: 429, body: `{"message":"rate limited","code":42910000}`}
	transport := &fakeTransport{responses: []fakeResponse{rateLimited, rateLimited, rateLimited}}
	c := newTestClient(t, transport, func(cfg *ClientConfig) { cfg.MaxAttempts = 3 })
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRetriesExhausted)
	assert.Equal(t, 3, transport.requestCount())
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
			transport := &fakeTransport{responses: []fakeResponse{{status: tc.status, body: `{"message":"denied","code":1}`}}}
			c := newTestClient(t, transport)
			_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.want)
			assert.Equal(t, 1, transport.requestCount(), "a permanent error must not be retried")
		})
	}
}

func TestFetchBars_MalformedJSONIsBadRequestAndNotRetried(t *testing.T) {
	transport := &fakeTransport{responses: []fakeResponse{{status: 200, body: "{not json"}}}
	c := newTestClient(t, transport)
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBadRequest)
	assert.Equal(t, 1, transport.requestCount(), "an undecodable response is permanent, not transient")
}

func TestFetchBars_CredentialsNeverAppearInError(t *testing.T) {
	transport := &fakeTransport{responses: []fakeResponse{{status: 401, body: `{"message":"invalid credentials","code":1}`}}}
	c := newTestClient(t, transport)
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), testKeyID)
	assert.NotContains(t, err.Error(), testSecretKey)
}

func TestFetchBars_RejectsEmptySymbol(t *testing.T) {
	c := newTestClient(t, &fakeTransport{})
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "  ", From: testFrom, To: testTo})
	assert.ErrorIs(t, err, ErrBadRequest)
}

func TestFetchBars_RejectsInvalidRange(t *testing.T) {
	c := newTestClient(t, &fakeTransport{})
	_, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testTo, To: testFrom})
	assert.ErrorIs(t, err, ErrBadRequest)
}

func TestFetchBars_AlreadyCancelledContext(t *testing.T) {
	c := newTestClient(t, &fakeTransport{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.FetchBars(ctx, BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	assert.ErrorIs(t, err, context.Canceled)
}

// cancelAfterTransport wraps a fakeTransport and cancels ctx exactly
// after its afterN'th call — mirrors the previous cancelAfterDoer's
// identical shape, adapted to http.RoundTripper (issue #323).
type cancelAfterTransport struct {
	inner  *fakeTransport
	cancel context.CancelFunc
	afterN int
	calls  int
}

func (d *cancelAfterTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	d.calls++
	resp, err := d.inner.RoundTrip(req)
	if d.calls == d.afterN {
		d.cancel()
	}
	return resp, err
}

// TestFetchBars_CancelledBetweenRetryAttempts is this package's
// cancellation contract after issue #323's SDK migration: the SDK's
// GetBars owns pagination internally and accepts no context.Context
// (see Client's own doc comment), so cancellation cannot interrupt a
// single SDK call, or one of its internal pages, once issued. What
// remains fully under this package's control — and therefore fully
// testable — is cancellation checked before each outer retry attempt.
// Canceling deterministically inside the first call's own RoundTrip
// (rather than racing a wall-clock sleep against a background
// goroutine) guarantees ctx is already Done by the time FetchBars's
// loop reaches its second iteration.
func TestFetchBars_CancelledBetweenRetryAttempts(t *testing.T) {
	inner := &fakeTransport{responses: []fakeResponse{
		{status: 503, body: `{"message":"unavailable","code":1}`},
		{status: 200, body: barsJSON("SPY", []string{"2024-01-02"}, "")},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	transport := &cancelAfterTransport{inner: inner, cancel: cancel, afterN: 1}
	c := newTestClient(t, transport, func(cfg *ClientConfig) { cfg.MaxAttempts = 5 })

	_, err := c.FetchBars(ctx, BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, inner.requestCount(), "must not issue a retried attempt after cancellation")
}

// notifyOnceTransport closes notify the first time RoundTrip
// completes, letting a test synchronize precisely on "the first HTTP
// call has finished" without a real-time sleep.
type notifyOnceTransport struct {
	inner  http.RoundTripper
	once   sync.Once
	notify chan struct{}
}

func (n *notifyOnceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := n.inner.RoundTrip(req)
	n.once.Do(func() { close(n.notify) })
	return resp, err
}

// TestFetchBars_CancelledDuringRetryBackoff exercises the other half
// of FetchBars' cancellation contract from
// TestFetchBars_CancelledBetweenRetryAttempts: cancellation arriving
// while genuinely waiting out the backoff delay between two attempts
// (not merely checked at the top of a loop iteration). RetryBaseDelay
// is set absurdly long and the injected clock is a clock.Simulated
// that is never advanced, so the only way the backoff's internal
// select can proceed at all is via ctx.Done() — a real timer firing on
// its own is impossible in this test, making the assertion fully
// deterministic rather than a real-time race.
func TestFetchBars_CancelledDuringRetryBackoff(t *testing.T) {
	attempt1Done := make(chan struct{})
	inner := &fakeTransport{responses: []fakeResponse{
		{status: 503, body: `{"message":"unavailable","code":1}`},
	}}
	transport := &notifyOnceTransport{inner: inner, notify: attempt1Done}
	simClock := clock.NewSimulated(time.Now())
	c := newTestClient(t, transport, func(cfg *ClientConfig) {
		cfg.MaxAttempts = 3
		cfg.RetryBaseDelay = time.Hour
		cfg.clock = simClock
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.FetchBars(ctx, BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
		done <- err
	}()

	<-attempt1Done
	cancel()

	err := <-done
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, inner.requestCount(), "must not issue a second attempt once backoff is cancelled")
}

// TestFetchBars_CredentialErrorIsNotRetried proves a failure resolving
// credentials propagates immediately without ever reaching the
// transport, and without being classified as transient (a credential
// resolution failure is not one of Client's sentinel errors, so
// isTransient correctly reports false for it).
func TestFetchBars_CredentialErrorIsNotRetried(t *testing.T) {
	transport := &fakeTransport{}
	credErr := errors.New("boom: credential store unavailable")
	cfg := ClientConfig{
		BaseURL:        "https://fake.example.com",
		Credential:     failingCredential{err: credErr},
		RetryBaseDelay: time.Millisecond,
		HTTPClient:     &http.Client{Transport: transport},
	}
	c, err := NewClient(cfg)
	require.NoError(t, err)

	_, err = c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: testFrom, To: testTo})
	require.Error(t, err)
	assert.ErrorIs(t, err, credErr)
	assert.Equal(t, 0, transport.requestCount(), "must never reach the transport when credentials fail to resolve")
}

type failingCredential struct{ err error }

func (f failingCredential) Credentials(context.Context) (string, string, error) {
	return "", "", f.err
}

// TestClassifyStatus_TruncatesLongBody proves an oversized diagnostic
// body excerpt is bounded to 4KB rather than growing an error message
// without limit.
func TestClassifyStatus_TruncatesLongBody(t *testing.T) {
	long := strings.Repeat("x", 8*1024)
	err := classifyStatus(http.StatusInternalServerError, long)
	require.Error(t, err)
	assert.LessOrEqual(t, len(err.Error()), 4*1024+128)
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
