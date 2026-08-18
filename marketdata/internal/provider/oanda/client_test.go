package oanda

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
		clock:          clock.Real{},
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

// --- Tests ---

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

	// Second page's "from" must advance past the last candle of page 1.
	secondFrom := doer.requests[1].URL.Query().Get("from")
	wantFrom := page1[len(page1)-1].Add(time.Nanosecond).Format(time.RFC3339Nano)
	assert.Equal(t, wantFrom, secondFrom)
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

func TestFetchCandles_TokenNeverAppearsInError(t *testing.T) {
	doer := &fakeDoer{responses: []fakeResponse{{status: 401, body: "invalid credentials, please check your token"}}}
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

func TestFixedIntervalLimiter_WaitsForInterval(t *testing.T) {
	cl := clock.NewSimulated(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	l := &fixedIntervalLimiter{clock: cl, interval: 100 * time.Millisecond}

	require.NoError(t, l.Wait(context.Background()))

	done := make(chan error, 1)
	go func() { done <- l.Wait(context.Background()) }()

	time.Sleep(20 * time.Millisecond) // let the goroutine reach the timer wait
	require.NoError(t, cl.Advance(100*time.Millisecond))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after Advance")
	}
}

func TestFixedIntervalLimiter_ZeroIntervalNeverWaits(t *testing.T) {
	l := &fixedIntervalLimiter{clock: clock.Real{}, interval: 0}
	require.NoError(t, l.Wait(context.Background()))
	require.NoError(t, l.Wait(context.Background()))
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

func TestStaticCredential(t *testing.T) {
	tok, err := StaticCredential("abc").Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "abc", tok)
}
