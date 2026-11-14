package oanda

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// cancelAfterDoer cancels a context after its Nth Do call, letting a
// test exercise cancellation arriving between pagination pages.
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

// candlesJSON builds a price=BA candles response body for times, all
// sharing the same fixed bid/ask OHLC values (sufficient for pagination/
// parsing tests, which only care about Time/Complete/count).
func candlesJSON(times []time.Time, complete bool) string {
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

const testToken = "test-secret-token-abc123"

func newTestClient(t *testing.T, doer HTTPDoer, opts ...func(*ClientConfig)) *Client {
	t.Helper()
	cfg := ClientConfig{
		BaseURL:        "https://fake.example.com",
		Credential:     StaticCredential(testToken),
		RetryBaseDelay: time.Millisecond,
		HTTPClient:     doer,
		Sleep:          func(context.Context, time.Duration) error { return nil },
	}
	for _, o := range opts {
		o(&cfg)
	}
	c, err := NewClient(cfg)
	require.NoError(t, err)
	return c
}

func withPageSize(n int) func(*ClientConfig) {
	return func(c *ClientConfig) { c.pageSize = n }
}

func TestFetchCandles_SinglePage(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: candlesJSON([]time.Time{
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
		}, true)},
	}}
	c := newTestClient(t, doer)

	records, err := c.FetchCandles(context.Background(), CandleRequest{
		Symbol:   "EURUSD",
		Interval: RawH1,
		From:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.True(t, records[0].Time.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, "1.1", records[0].BidOpen.String())
	assert.Equal(t, "1.1006", records[0].AskClose.String())
	assert.True(t, records[0].Complete)
	assert.Equal(t, int64(100), records[0].Volume)

	require.Equal(t, 1, doer.requestCount())
	q := doer.lastRequest().URL.Query()
	assert.Equal(t, "/v3/instruments/EUR_USD/candles", doer.lastRequest().URL.Path)
	assert.Equal(t, "H1", q.Get("granularity"))
	assert.Equal(t, "BA", q.Get("price"))
	assert.Equal(t, "17", q.Get("dailyAlignment"))
	assert.Equal(t, "America/New_York", q.Get("alignmentTimezone"))
	assert.Equal(t, "Bearer "+testToken, doer.lastRequest().Header.Get("Authorization"))
}

func TestFetchCandles_GranularityD1(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 200, body: candlesJSON(nil, true)}}}
	c := newTestClient(t, doer)
	_, err := c.FetchCandles(context.Background(), CandleRequest{
		Symbol: "EURUSD", Interval: RawD1,
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Equal(t, "D", doer.lastRequest().URL.Query().Get("granularity"))
}

func TestFetchCandles_Paginates(t *testing.T) {
	page1 := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
	}
	page2 := []time.Time{
		time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC),
	}
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: candlesJSON(page1, true)}, // full page (size 2) -> triggers page 2
		{status: 200, body: candlesJSON(page2, true)}, // short page -> stop
	}}
	c := newTestClient(t, doer, withPageSize(2))

	records, err := c.FetchCandles(context.Background(), CandleRequest{
		Symbol: "EURUSD", Interval: RawH1,
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, records, 3)
	assert.Equal(t, 2, doer.requestCount())

	// The first page includes a candle opening at from; the second continues from
	// the last candle and asks the provider to leave it out.
	assert.Equal(t, "true", doer.requests[0].URL.Query().Get("includeFirst"))
	assert.Equal(t, page1[len(page1)-1].Format(time.RFC3339Nano), doer.requests[1].URL.Query().Get("from"))
	assert.Equal(t, "false", doer.requests[1].URL.Query().Get("includeFirst"))
}

// A provider that repeats the last candle of the previous page is a malformed
// stream, not progress.
func TestFetchCandles_RejectsAPageThatRepeatsThePreviousCandle(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: candlesJSON([]time.Time{t0, t0.Add(time.Hour)}, true)},
		{status: 200, body: candlesJSON([]time.Time{t0.Add(time.Hour), t0.Add(2 * time.Hour)}, true)},
	}}
	c := newTestClient(t, doer, withPageSize(2))

	got, err := c.FetchCandles(context.Background(), hourRange())

	require.ErrorIs(t, err, ErrMalformedData)
	assert.Len(t, got, 2, "the records fetched before the failure are returned")
}

// OANDA also returns the candle covering an unaligned from; it opened before the
// requested range and is dropped, and a full page is still recognised as full.
func TestFetchCandles_DropsTheCandleCoveringAnUnalignedFrom(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: candlesJSON([]time.Time{t0, t0.Add(time.Hour)}, true)},
		{status: 200, body: candlesJSON([]time.Time{t0.Add(2 * time.Hour)}, true)},
	}}
	c := newTestClient(t, doer, withPageSize(2))
	req := hourRange()
	req.From = t0.Add(30 * time.Minute)

	got, err := c.FetchCandles(context.Background(), req)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, got[0].Time.Equal(t0.Add(time.Hour)))
	assert.Equal(t, 2, doer.requestCount(), "the trimmed first page was full, so the next page is requested")
}

func TestFetchCandles_RequiresACandlesArray(t *testing.T) {
	for name, body := range map[string]string{
		"empty object": `{}`,
		"null":         `null`,
		"null array":   `{"candles":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, &fakeDoer{responses: []fakeResponse{{status: 200, body: body}}})

			_, err := c.FetchCandles(context.Background(), hourRange())

			assert.ErrorIs(t, err, ErrMalformedData)
		})
	}
}

func TestFetchCandles_AnExplicitEmptyArrayIsAnEmptyRange(t *testing.T) {
	c := newTestClient(t, &fakeDoer{responses: []fakeResponse{{status: 200, body: `{"candles":[]}`}}})

	got, err := c.FetchCandles(context.Background(), hourRange())

	require.NoError(t, err)
	assert.Empty(t, got)
}

// stallBody blocks reading until the context is cancelled, like a response body
// that stops arriving.
type stallBody struct{ ctx context.Context }

func (b stallBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }

func (stallBody) Close() error { return nil }

type stallDoer struct{ cancel context.CancelFunc }

func (d stallDoer) Do(req *http.Request) (*http.Response, error) {
	go d.cancel()
	return &http.Response{StatusCode: 200, Body: stallBody{req.Context()}, Header: http.Header{}}, nil
}

func TestFetchCandles_CancellationWhileReadingTheBodyIsNotMalformedData(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newTestClient(t, stallDoer{cancel})

	_, err := c.FetchCandles(ctx, hourRange())

	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, ErrMalformedData)
}

// The default transport returns a redirect instead of following it, so the
// bearer token is never forwarded to another URL.
func TestDefaultTransportDoesNotFollowRedirectsWithTheToken(t *testing.T) {
	var targetAuth string
	var targetHit bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHit, targetAuth = true, r.Header.Get("Authorization")
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
	}))
	defer redirector.Close()
	c, err := NewClient(ClientConfig{BaseURL: redirector.URL, Credential: StaticCredential(testToken), Sleep: func(context.Context, time.Duration) error { return nil }})
	require.NoError(t, err)

	_, err = c.FetchCandles(context.Background(), hourRange())

	require.Error(t, err)
	assert.False(t, targetHit, "the redirect target was never contacted")
	assert.Empty(t, targetAuth)
	assert.NotContains(t, err.Error(), testToken)
}

func TestFetchCandles_StopsAtToBoundary(t *testing.T) {
	to := time.Date(2024, 1, 1, 1, 30, 0, 0, time.UTC)
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: candlesJSON([]time.Time{
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
			time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC), // >= to: excluded
		}, true)},
	}}
	c := newTestClient(t, doer)
	records, err := c.FetchCandles(context.Background(), CandleRequest{
		Symbol: "EURUSD", Interval: RawH1,
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: to,
	})
	require.NoError(t, err)
	require.Len(t, records, 2)
}

func TestFetchCandles_RetriesTransientThenSucceeds(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{err: errors.New("connection reset")},
		{status: 503, body: "service unavailable"},
		{status: 200, body: candlesJSON([]time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, true)},
	}}
	c := newTestClient(t, doer)
	records, err := c.FetchCandles(context.Background(), CandleRequest{
		Symbol: "EURUSD", Interval: RawH1,
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, 3, doer.requestCount())
}

func TestFetchCandles_RetriesExhausted(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 500, body: "err1"},
		{status: 500, body: "err2"},
		{status: 500, body: "err3"},
	}}
	c := newTestClient(t, doer, func(cfg *ClientConfig) { cfg.MaxAttempts = 3 })
	_, err := c.FetchCandles(context.Background(), CandleRequest{
		Symbol: "EURUSD", Interval: RawH1,
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRetriesExhausted)
	assert.Equal(t, 3, doer.requestCount())
}

func TestFetchCandles_PermanentErrorNotRetried(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"forbidden", http.StatusForbidden, ErrUnauthorized},
		{"bad request", http.StatusBadRequest, ErrBadRequest},
		{"not found", http.StatusNotFound, ErrBadRequest},
		{"teapot", http.StatusTeapot, ErrUnexpectedStatus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doer := &fakeDoer{responses: []fakeResponse{{status: tc.status, body: "denied"}}}
			c := newTestClient(t, doer)
			_, err := c.FetchCandles(context.Background(), CandleRequest{
				Symbol: "EURUSD", Interval: RawH1,
				From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.want)
			assert.Equal(t, 1, doer.requestCount(), "a permanent error must not be retried")
		})
	}
}

func TestFetchCandles_A401IsRetriedAndASecondTryCanSucceed(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 401, body: "Insufficient authorization to perform request."},
		{status: 200, body: candlesJSON([]time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, true)},
	}}
	c := newTestClient(t, doer)

	got, err := c.FetchCandles(context.Background(), hourRange())

	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, 2, doer.requestCount())
}

func TestFetchCandles_AnAlways401IsExhaustedAndStillUnauthorized(t *testing.T) {
	r := fakeResponse{status: 401, body: "no"}
	doer := &fakeDoer{responses: []fakeResponse{r, r, r, r}}
	c := newTestClient(t, doer)

	_, err := c.FetchCandles(context.Background(), hourRange())

	require.ErrorIs(t, err, ErrUnauthorized)
	require.ErrorIs(t, err, ErrRetriesExhausted)
	assert.Equal(t, 3, doer.requestCount(), "bounded by the attempt limit")
}

func TestFetchCandles_A403IsNeverRetried(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 403, body: "no"}, {status: 200, body: candlesJSON(nil, true)}}}
	c := newTestClient(t, doer)

	_, err := c.FetchCandles(context.Background(), hourRange())

	require.ErrorIs(t, err, ErrUnauthorized)
	assert.NotErrorIs(t, err, ErrRetriesExhausted)
	assert.Equal(t, 1, doer.requestCount())
}

func TestFetchCandles_TokenNeverAppearsInError(t *testing.T) {
	body := "invalid credentials, please check your token"
	doer := &fakeDoer{responses: []fakeResponse{{status: 401, body: body}, {status: 401, body: body}, {status: 401, body: body}}}
	c := newTestClient(t, doer)
	_, err := c.FetchCandles(context.Background(), CandleRequest{
		Symbol: "EURUSD", Interval: RawH1,
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), testToken)
}

func TestFetchCandles_RejectsOutOfScopeSymbol(t *testing.T) {
	c := newTestClient(t, &fakeDoer{})
	_, err := c.FetchCandles(context.Background(), CandleRequest{
		Symbol: "XAUUSD", Interval: RawH1,
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	assert.ErrorIs(t, err, ErrInstrumentOutOfScope)
}

func TestFetchCandles_RejectsInvalidRange(t *testing.T) {
	c := newTestClient(t, &fakeDoer{})
	_, err := c.FetchCandles(context.Background(), CandleRequest{
		Symbol: "EURUSD", Interval: RawH1,
		From: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	assert.ErrorIs(t, err, ErrBadRequest)
}

func TestFetchCandles_AlreadyCancelledContext(t *testing.T) {
	c := newTestClient(t, &fakeDoer{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.FetchCandles(ctx, CandleRequest{
		Symbol: "EURUSD", Interval: RawH1,
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFetchCandles_CancelledMidPagination(t *testing.T) {
	inner := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: candlesJSON([]time.Time{
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
		}, true)},
		{status: 200, body: candlesJSON([]time.Time{time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC)}, true)},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	doer := &cancelAfterDoer{inner: inner, cancel: cancel, afterN: 1}
	c := newTestClient(t, doer, withPageSize(2))

	_, err := c.FetchCandles(ctx, CandleRequest{
		Symbol: "EURUSD", Interval: RawH1,
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, inner.requestCount(), "must not issue the second page's request after cancellation")
}

func TestNewClient_RequiresBaseURLAndCredential(t *testing.T) {
	_, err := NewClient(ClientConfig{Credential: StaticCredential("x")})
	assert.Error(t, err)
	_, err = NewClient(ClientConfig{BaseURL: "https://x"})
	assert.Error(t, err)
}

func TestToAPISymbol(t *testing.T) {
	got, err := toAPISymbol("EURUSD")
	require.NoError(t, err)
	assert.Equal(t, "EUR_USD", got)

	_, err = toAPISymbol("XAUUSD")
	assert.ErrorIs(t, err, ErrInstrumentOutOfScope)

	_, err = toAPISymbol("bad")
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestToGranularity(t *testing.T) {
	cases := map[RawInterval]string{RawM1: "M1", RawH1: "H1", RawH4: "H4", RawD1: "D"}
	for interval, want := range cases {
		got, err := toGranularity(interval)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := toGranularity(RawInterval("w1"))
	assert.ErrorIs(t, err, ErrUnsupportedInterval)
}

func TestNewClient_BaseURLMayCarryTheV3Suffix(t *testing.T) {
	for _, base := range []string{
		"https://api-fxtrade.oanda.com",
		"https://api-fxtrade.oanda.com/",
		"https://api-fxtrade.oanda.com/v3",
		"https://api-fxtrade.oanda.com/v3/",
	} {
		c, err := NewClient(ClientConfig{BaseURL: base, Credential: StaticCredential("x")})
		require.NoError(t, err, base)
		assert.Equal(t, "https://api-fxtrade.oanda.com", c.baseURL, base)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://api-fxtrade.oanda.com/v3":  "https://api-fxtrade.oanda.com",
		"https://api-fxtrade.oanda.com/v3/": "https://api-fxtrade.oanda.com",
		"https://api-fxtrade.oanda.com":     "https://api-fxtrade.oanda.com",
		"https://v3":                        "https://v3",
		"https://v3/v3":                     "https://v3",
		"https://proxy.example/oanda/v3":    "https://proxy.example/oanda",
		"http://localhost:8080/":            "http://localhost:8080",
		"https://api.example/v3/extra":      "https://api.example/v3/extra",
		"not a url":                         "not a url",
	}
	for in, want := range cases {
		assert.Equal(t, want, normalizeBaseURL(in), in)
	}
}

// recordingSleeper records every requested delay and returns at once.
type recordingSleeper struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (r *recordingSleeper) sleep(_ context.Context, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.delays = append(r.delays, d)
	return nil
}

func hourRange() CandleRequest {
	return CandleRequest{
		Symbol: "EURUSD", Interval: RawH1,
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func TestFetchCandles_RetryBackoffDoublesAndNeverReallyWaits(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 429, body: "slow down"}, {status: 503, body: "x"}, {status: 500, body: "y"}, {status: 502, body: "z"},
	}}
	rs := &recordingSleeper{}
	c := newTestClient(t, doer, func(cfg *ClientConfig) {
		cfg.MaxAttempts = 4
		cfg.RetryBaseDelay = time.Second
		cfg.Sleep = rs.sleep
	})
	start := time.Now()

	_, err := c.FetchCandles(context.Background(), hourRange())

	require.ErrorIs(t, err, ErrRetriesExhausted)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, rs.delays)
	assert.Less(t, time.Since(start), time.Second, "the injected sleeper means the test does not wait")
}

func TestFetchCandles_BackoffIsCapped(t *testing.T) {
	var responses []fakeResponse
	for range 12 {
		responses = append(responses, fakeResponse{status: 503})
	}
	rs := &recordingSleeper{}
	c := newTestClient(t, &fakeDoer{responses: responses}, func(cfg *ClientConfig) {
		cfg.MaxAttempts = 12
		cfg.RetryBaseDelay = time.Second
		cfg.Sleep = rs.sleep
	})

	_, err := c.FetchCandles(context.Background(), hourRange())

	require.ErrorIs(t, err, ErrRetriesExhausted)
	assert.Equal(t, 64*time.Second, rs.delays[len(rs.delays)-1])
}

func TestFetchCandles_CancelledDuringBackoff(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 503}, {status: 503}}}
	ctx, cancel := context.WithCancel(context.Background())
	c := newTestClient(t, doer, func(cfg *ClientConfig) {
		cfg.Sleep = func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		}
	})

	_, err := c.FetchCandles(ctx, hourRange())

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, doer.requestCount())
}

func TestRealSleep(t *testing.T) {
	require.NoError(t, realSleep(context.Background(), time.Millisecond))
	require.NoError(t, realSleep(context.Background(), 0))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, realSleep(ctx, time.Hour), context.Canceled)
	assert.ErrorIs(t, realSleep(ctx, 0), context.Canceled)
}

func TestPaceLimiter(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rs := &recordingSleeper{}
	l := &paceLimiter{now: func() time.Time { return now }, sleep: rs.sleep, interval: 100 * time.Millisecond}

	require.NoError(t, l.wait(context.Background()))
	assert.Empty(t, rs.delays, "the first request is not delayed")
	now = now.Add(30 * time.Millisecond)
	require.NoError(t, l.wait(context.Background()))
	assert.Equal(t, []time.Duration{70 * time.Millisecond}, rs.delays)
	now = now.Add(time.Second)
	require.NoError(t, l.wait(context.Background()))
	assert.Len(t, rs.delays, 1, "a request after the interval is not delayed")
}

func TestPaceLimiter_DisabledAndNilNeverWait(t *testing.T) {
	var nilLimiter *paceLimiter
	require.NoError(t, nilLimiter.wait(context.Background()))
	l := &paceLimiter{interval: 0}
	require.NoError(t, l.wait(context.Background()))
}

func TestPaceLimiter_SleepErrorPropagates(t *testing.T) {
	now := time.Now()
	boom := errors.New("boom")
	l := &paceLimiter{now: func() time.Time { return now }, sleep: func(context.Context, time.Duration) error { return boom }, interval: time.Hour}
	require.NoError(t, l.wait(context.Background()))

	assert.ErrorIs(t, l.wait(context.Background()), boom)
}

// Concurrent callers are paced against each other and the limiter is race-free.
func TestPaceLimiter_ConcurrentWaitIsRaceFree(t *testing.T) {
	rs := &recordingSleeper{}
	l := &paceLimiter{now: time.Now, sleep: rs.sleep, interval: time.Millisecond}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { assert.NoError(t, l.wait(context.Background())) })
	}
	wg.Wait()
}

func TestNewClient_PacesWhenAnIntervalIsConfigured(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: candlesJSON([]time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)}, true)},
		{status: 200, body: candlesJSON([]time.Time{time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC)}, true)},
	}}
	rs := &recordingSleeper{}
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := newTestClient(t, doer, withPageSize(2), func(cfg *ClientConfig) {
		cfg.MinRequestInterval = time.Second
		cfg.Sleep = rs.sleep
		cfg.Now = func() time.Time { return now }
	})

	_, err := c.FetchCandles(context.Background(), hourRange())

	require.NoError(t, err)
	assert.Equal(t, []time.Duration{time.Second}, rs.delays, "the second page waits out the interval")
}

func TestNewClient_BaseURLRules(t *testing.T) {
	cred := StaticCredential("x")
	for base, ok := range map[string]bool{
		"https://api-fxpractice.oanda.com": true,
		"http://127.0.0.1:8080":            true,
		"http://localhost:9":               true,
		"http://[::1]:8080":                true,
		"http://api-fxpractice.oanda.com":  false,
		"http://10.0.0.5":                  false,
		"ftp://api-fxpractice.oanda.com":   false,
		"https://user:pass@api.oanda.com":  false,
		"api-fxpractice.oanda.com":         false,
		"https://":                         false,
	} {
		_, err := NewClient(ClientConfig{BaseURL: base, Credential: cred})
		if ok {
			assert.NoError(t, err, base)
		} else {
			assert.Error(t, err, base)
			assert.NotContains(t, err.Error(), "pass", "an error never echoes the userinfo")
		}
	}
}

const echoToken = "tok-" + "ECHO-9f8e7d"

func TestFetchCandles_TokenEchoedByTheProviderIsScrubbed(t *testing.T) {
	for name, resp := range map[string]fakeResponse{
		"401 body echo": {status: 401, body: "bad token " + echoToken},
		"400 body echo": {status: 400, body: "Authorization: Bearer " + echoToken},
		"network error": {err: errors.New("dial https://x/?auth=" + echoToken + ": refused")},
	} {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, &fakeDoer{responses: []fakeResponse{resp, resp, resp}}, func(cfg *ClientConfig) {
				cfg.Credential = StaticCredential(echoToken)
			})

			_, err := c.FetchCandles(context.Background(), hourRange())

			require.Error(t, err)
			assert.NotContains(t, err.Error(), echoToken)
			assert.Contains(t, err.Error(), "[redacted]")
		})
	}
}

func TestScrubbedErrorKeepsItsClassification(t *testing.T) {
	err := scrubError(fmt.Errorf("%w: %s", ErrUnauthorized, echoToken), echoToken)

	assert.ErrorIs(t, err, ErrUnauthorized)
	assert.NotErrorIs(t, err, ErrBadRequest)
	assert.NotContains(t, err.Error(), echoToken)
}

func TestScrubErrorLeavesACleanErrorAlone(t *testing.T) {
	orig := errors.New("plain")

	assert.Same(t, orig, scrubError(orig, echoToken))
	assert.Same(t, orig, scrubError(orig, ""))
}

func TestFetchCandles_TokenNeverAppearsInLogs(t *testing.T) {
	logger, rec := logging.Capture()
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 503, body: "echo " + echoToken},
		{status: 200, body: candlesJSON([]time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, true)},
	}}
	c := newTestClient(t, doer, func(cfg *ClientConfig) {
		cfg.Credential = StaticCredential(echoToken)
		cfg.Logger = logger
	})

	_, err := c.FetchCandles(context.Background(), hourRange())

	require.NoError(t, err)
	require.NotEmpty(t, rec.Records())
	for _, r := range rec.Records() {
		assert.NotContains(t, fmt.Sprint(r.Message, r.Attrs), echoToken)
	}
	msgs := []string{}
	for _, r := range rec.Records() {
		msgs = append(msgs, r.Message)
	}
	assert.Contains(t, msgs, "retrying")
	assert.Contains(t, msgs, "fetch finished")
	assert.Equal(t, "marketdata", rec.Records()[0].Attrs[logging.Component])
}

func TestFetchCandles_NilLoggerIsSilent(t *testing.T) {
	c := newTestClient(t, &fakeDoer{responses: []fakeResponse{{status: 200, body: candlesJSON(nil, true)}}})

	_, err := c.FetchCandles(context.Background(), hourRange())

	assert.NoError(t, err)
}

func TestFetchCandles_EmptyTokenIsRefusedBeforeAnyRequest(t *testing.T) {
	doer := &fakeDoer{}
	c := newTestClient(t, doer, func(cfg *ClientConfig) { cfg.Credential = StaticCredential("  ") })

	_, err := c.FetchCandles(context.Background(), hourRange())

	require.ErrorIs(t, err, ErrUnauthorized)
	assert.Equal(t, 0, doer.requestCount())
}

type failingCredential struct{}

func (failingCredential) Token(context.Context) (string, error) { return "", errors.New("vault down") }

func TestFetchCandles_CredentialErrorIsReportedAndNotRetried(t *testing.T) {
	doer := &fakeDoer{}
	c := newTestClient(t, doer, func(cfg *ClientConfig) { cfg.Credential = failingCredential{} })

	_, err := c.FetchCandles(context.Background(), hourRange())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault down")
	assert.Equal(t, 0, doer.requestCount())
}

func TestFetchCandles_RejectsMalformedPages(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, body := range map[string]string{
		"not json":    "<html>",
		"descending":  candlesJSON([]time.Time{t0.Add(time.Hour), t0}, true),
		"duplicate":   candlesJSON([]time.Time{t0, t0}, true),
		"bad time":    `{"candles":[{"complete":true,"time":"nope","volume":1,"bid":{"o":"1","h":"1","l":"1","c":"1"},"ask":{"o":"1","h":"1","l":"1","c":"1"}}]}`,
		"bad price":   `{"candles":[{"complete":true,"time":"2024-01-01T00:00:00Z","volume":1,"bid":{"o":"x","h":"1","l":"1","c":"1"},"ask":{"o":"1","h":"1","l":"1","c":"1"}}]}`,
		"missing ask": `{"candles":[{"complete":true,"time":"2024-01-01T00:00:00Z","volume":1,"bid":{"o":"1","h":"1","l":"1","c":"1"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			doer := &fakeDoer{responses: []fakeResponse{{status: 200, body: body}}}
			c := newTestClient(t, doer)

			_, err := c.FetchCandles(context.Background(), hourRange())

			require.ErrorIs(t, err, ErrMalformedData)
			assert.Equal(t, 1, doer.requestCount(), "a malformed page is permanent, not retried")
		})
	}
}

func TestFetchCandles_OversizedResponseIsRejected(t *testing.T) {
	big := `{"candles":[],"pad":"` + strings.Repeat("a", maxResponseBytes+10) + `"}`
	c := newTestClient(t, &fakeDoer{responses: []fakeResponse{{status: 200, body: big}}})

	_, err := c.FetchCandles(context.Background(), hourRange())

	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestFetchCandles_ReturnsTheIncompleteLastCandleAsDeclared(t *testing.T) {
	last := time.Date(2024, 1, 1, 5, 0, 0, 0, time.UTC)
	body := candlesJSON([]time.Time{time.Date(2024, 1, 1, 4, 0, 0, 0, time.UTC)}, true)
	body = body[:len(body)-2] + "," + candlesJSON([]time.Time{last}, false)[len(`{"candles":[`):]
	c := newTestClient(t, &fakeDoer{responses: []fakeResponse{{status: 200, body: body}}})

	got, err := c.FetchCandles(context.Background(), hourRange())

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, got[0].Complete)
	assert.False(t, got[1].Complete)
}

func TestFetchCandles_ReturnsRecordsFetchedBeforeAFailure(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{
		{status: 200, body: candlesJSON([]time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)}, true)},
		{status: 403, body: "no"},
	}}
	c := newTestClient(t, doer, withPageSize(2))

	got, err := c.FetchCandles(context.Background(), hourRange())

	require.ErrorIs(t, err, ErrUnauthorized)
	assert.Len(t, got, 2)
}

func TestFetchCandles_NetworkErrorAfterCancellationReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	doer := &cancelAfterDoer{inner: &fakeDoer{responses: []fakeResponse{{err: errors.New("context canceled by transport")}}}, cancel: cancel, afterN: 1}
	c := newTestClient(t, doer)

	_, err := c.FetchCandles(ctx, hourRange())

	assert.ErrorIs(t, err, context.Canceled)
}

func TestFetchCandles_OverRealHTTP(t *testing.T) {
	var gotAuth, gotPath string
	var gotQuery map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		gotQuery = map[string]string{}
		for k, v := range r.URL.Query() {
			gotQuery[k] = v[0]
		}
		_, _ = io.WriteString(w, candlesJSON([]time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, true))
	}))
	defer srv.Close()
	c, err := NewClient(ClientConfig{BaseURL: srv.URL + "/v3", Credential: StaticCredential(testToken), HTTPClient: srv.Client()})
	require.NoError(t, err)
	req := hourRange()
	req.Interval = RawD1

	got, err := c.FetchCandles(context.Background(), req)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Bearer "+testToken, gotAuth)
	assert.Equal(t, "/v3/instruments/EUR_USD/candles", gotPath)
	assert.Equal(t, "D", gotQuery["granularity"])
	assert.Equal(t, "BA", gotQuery["price"])
	assert.Equal(t, "5000", gotQuery["count"])
	assert.Equal(t, "17", gotQuery["dailyAlignment"])
	assert.Equal(t, "America/New_York", gotQuery["alignmentTimezone"])
	assert.Equal(t, "2024-01-01T00:00:00Z", gotQuery["from"])
	assert.NotContains(t, srv.URL, testToken)
}

func TestFetchCandles_ParsesPricesExactly(t *testing.T) {
	c := newTestClient(t, &fakeDoer{responses: []fakeResponse{
		{status: 200, body: candlesJSON([]time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, true)},
	}})

	got, err := c.FetchCandles(context.Background(), hourRange())

	require.NoError(t, err)
	assert.Equal(t, "1.1", got[0].BidOpen.String())
	assert.Equal(t, int64(100), got[0].Volume)
}

func TestClassifyStatus_RateLimitedIsTransient(t *testing.T) {
	err := classifyStatus(429, strings.NewReader("slow"))

	assert.True(t, isTransient(err))
	assert.True(t, isTransient(classifyStatus(401, strings.NewReader("no"))), "a 401 is retried (#624)")
	assert.False(t, isTransient(classifyStatus(403, strings.NewReader("no"))))
}

func TestFetchCandles_RejectsDataAfterTheResponse(t *testing.T) {
	good := candlesJSON([]time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, true)
	for name, body := range map[string]string{
		"a second value":  good + good,
		"stray text":      good + " junk",
		"oversize padded": good + strings.Repeat(" ", maxResponseBytes),
	} {
		t.Run(name, func(t *testing.T) {
			doer := &fakeDoer{responses: []fakeResponse{{status: 200, body: body}}}
			c := newTestClient(t, doer)

			_, err := c.FetchCandles(context.Background(), hourRange())

			require.ErrorIs(t, err, ErrMalformedData)
			assert.Equal(t, 1, doer.requestCount(), "permanent, not retried")
		})
	}
}

func TestFetchCandles_TrailingWhitespaceIsAccepted(t *testing.T) {
	body := candlesJSON([]time.Time{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, true) + "\n"
	c := newTestClient(t, &fakeDoer{responses: []fakeResponse{{status: 200, body: body}}})

	got, err := c.FetchCandles(context.Background(), hourRange())

	require.NoError(t, err)
	assert.Len(t, got, 1)
}

// oandaFake models the provider's candles endpoint over an hourly series: it
// returns candles that open at or after from, plus the candle covering an
// unaligned from, includes a candle opening exactly at from only when
// includeFirst is true, and returns at most count candles.
type oandaFake struct {
	mu     sync.Mutex
	series []time.Time
	calls  int
}

func (f *oandaFake) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	q := req.URL.Query()
	from, err := time.Parse(time.RFC3339Nano, q.Get("from"))
	if err != nil {
		return nil, err
	}
	count, _ := strconv.Atoi(q.Get("count"))
	includeFirst := q.Get("includeFirst") != "false" // the provider's default is true
	var out []time.Time
	for _, t := range f.series {
		covers := t.Before(from) && t.Add(time.Hour).After(from)
		switch {
		case t.Equal(from) && includeFirst, t.After(from), covers && includeFirst:
			out = append(out, t)
		}
		if len(out) == count {
			break
		}
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(candlesJSON(out, true)))}, nil
}

func TestFetchCandles_AgainstProviderSemantics(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	series := make([]time.Time, 10)
	for i := range series {
		series[i] = t0.Add(time.Duration(i) * time.Hour)
	}
	for name, tc := range map[string]struct {
		from, to time.Time
		want     []time.Time
	}{
		"aligned, several pages":   {t0, t0.Add(10 * time.Hour), series},
		"unaligned from":           {t0.Add(90 * time.Minute), t0.Add(10 * time.Hour), series[2:]},
		"to cuts the range":        {t0, t0.Add(5 * time.Hour), series[:5]},
		"to unaligned":             {t0.Add(time.Hour), t0.Add(4*time.Hour + time.Minute), series[1:5]},
		"exactly a full last page": {t0, t0.Add(4 * time.Hour), series[:4]},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &oandaFake{series: series}
			c := newTestClient(t, fake, withPageSize(2))

			got, err := c.FetchCandles(context.Background(), CandleRequest{Symbol: "EURUSD", Interval: RawH1, From: tc.from, To: tc.to})

			require.NoError(t, err)
			var times []time.Time
			for _, r := range got {
				times = append(times, r.Time)
			}
			assert.Equal(t, tc.want, times)
		})
	}
}
