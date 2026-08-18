package oanda

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/num"
)

// Sentinel errors Client returns (wrapped), classifying a failed request
// without requiring a caller to inspect an HTTP status code directly
// (issue #80). A permanent error (ErrUnauthorized, ErrBadRequest) is
// never retried; a transient one (ErrRateLimited, ErrProviderUnavailable,
// or a network-level failure) is retried up to the Client's configured
// attempt limit, reported as ErrRetriesExhausted if every attempt fails.
var (
	// ErrUnauthorized marks an HTTP 401/403 response: the credential is
	// missing, invalid, or lacks access. Retrying with the same
	// credential cannot succeed.
	ErrUnauthorized = errors.New("oanda: unauthorized")
	// ErrBadRequest marks an HTTP 400/404 response: the request itself
	// (instrument, granularity, range) is malformed or refers to
	// something OANDA does not recognize. Retrying without changing the
	// request cannot succeed.
	ErrBadRequest = errors.New("oanda: bad request")
	// ErrRateLimited marks an HTTP 429 response.
	ErrRateLimited = errors.New("oanda: rate limited")
	// ErrProviderUnavailable marks an HTTP 5xx response.
	ErrProviderUnavailable = errors.New("oanda: provider unavailable")
	// ErrRetriesExhausted marks a request that failed transiently on
	// every attempt Client's retry policy allowed.
	ErrRetriesExhausted = errors.New("oanda: retries exhausted")
	// ErrUnexpectedStatus marks any other non-2xx status. Treated as
	// permanent (not retried): an unrecognized status is safer to
	// surface immediately than to retry indefinitely against.
	ErrUnexpectedStatus = errors.New("oanda: unexpected status")
)

// dailyAlignment and alignmentTimezone are pinned on every daily-or-
// coarser request rather than left to OANDA's account defaults — see
// the package doc comment ("Future live synchronization") and ADR-020:
// the preserved raw archive's D1 opens only happen to match a 17:00
// America/New_York rollover because the legacy client inherited that as
// an account default, not because it requested it. Pinning these
// explicitly means a future account-default change cannot silently
// misalign new data against the existing archive.
const (
	dailyAlignment    = "17"
	alignmentTimezone = "America/New_York"
)

// HTTPDoer is the minimal seam Client issues requests through. It is
// satisfied by *http.Client; tests inject a fake implementation instead,
// so no unit test in this package ever makes a real network call.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// rateLimiter paces outgoing requests. Client calls Wait immediately
// before every attempt, including retries, so backoff delay and rate-
// limit pacing compose rather than race each other.
type rateLimiter interface {
	Wait(ctx context.Context) error
}

// noopRateLimiter never delays. It is the zero-configuration default for
// callers that supply their own pacing upstream (or, in tests, want
// pagination/retry logic exercised without any real or simulated delay).
type noopRateLimiter struct{}

func (noopRateLimiter) Wait(context.Context) error { return nil }

// fixedIntervalLimiter enforces a minimum spacing between requests,
// timed through a clock.Clock rather than time.Sleep so tests can drive
// it deterministically with clock.Simulated.
type fixedIntervalLimiter struct {
	clock    clock.Clock
	interval time.Duration
	last     time.Time
}

func (l *fixedIntervalLimiter) Wait(ctx context.Context) error {
	if l.interval <= 0 {
		return nil
	}
	now := l.clock.Now()
	if !l.last.IsZero() {
		if wait := l.interval - now.Sub(l.last); wait > 0 {
			timer := l.clock.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C():
			}
			now = l.clock.Now()
		}
	}
	l.last = now
	return nil
}

// ClientConfig holds Client's explicit dependencies. Configuration is a
// composition-root concern, the same convention marketdata.Manager
// follows: Client never reads environment variables or files itself.
type ClientConfig struct {
	// BaseURL is OANDA's API base, for example
	// "https://api-fxpractice.oanda.com". Required. Deliberately a full
	// URL rather than a "practice"/"live" enum Client would parse
	// itself — environment selection is the composition root's typed
	// configuration decision, not domain-level string parsing.
	BaseURL string
	// Credential supplies the bearer token for every request. Required.
	Credential CredentialProvider

	// MaxAttempts bounds how many times a transiently-failing request is
	// attempted in total (the first try plus retries). Non-positive
	// selects a package default (3).
	MaxAttempts int
	// RetryBaseDelay is the delay before the first retry; each
	// subsequent retry doubles it. Non-positive selects a package
	// default (500ms).
	RetryBaseDelay time.Duration
	// MinRequestInterval enforces a minimum spacing between requests
	// when positive. Zero (the default) disables rate limiting at this
	// layer, leaving pacing to the caller or a future policy.
	MinRequestInterval time.Duration

	// HTTPClient overrides the transport Client issues requests through
	// (default http.DefaultClient). Exported — unlike clock and
	// pageSize below — specifically so a caller within the marketdata/
	// tree (Manager's own construction, or a test in package
	// marketdata) can inject a fake transport when building a Client
	// indirectly, without this package needing to expose a broader
	// testing surface than that.
	HTTPClient HTTPDoer

	// clock and pageSize are internal test seams, unexported so no
	// caller can inject a non-deterministic dependency or a non-
	// provider-accurate page size through Config. pageSize lets this
	// package's own tests exercise real pagination logic (a page
	// boundary, a short final page) without constructing thousands of
	// fixture candles.
	clock    clock.Clock
	pageSize int
}

const (
	defaultMaxAttempts    = 3
	defaultRetryBaseDelay = 500 * time.Millisecond
)

// Client is a minimal OANDA v20 REST client for historical candle
// download (issue #80). It is the only network-facing type in this
// package; nothing else here makes an HTTP request.
type Client struct {
	baseURL     string
	credential  CredentialProvider
	http        HTTPDoer
	clock       clock.Clock
	limiter     rateLimiter
	maxAttempts int
	retryBase   time.Duration
	pageSize    int
}

// NewClient validates cfg and returns a ready-to-use Client.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("oanda: new client: base URL is required")
	}
	if cfg.Credential == nil {
		return nil, fmt.Errorf("oanda: new client: credential is required")
	}
	cl := cfg.clock
	if cl == nil {
		cl = clock.Real{}
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	retryBase := cfg.RetryBaseDelay
	if retryBase <= 0 {
		retryBase = defaultRetryBaseDelay
	}
	var limiter rateLimiter = noopRateLimiter{}
	if cfg.MinRequestInterval > 0 {
		limiter = &fixedIntervalLimiter{clock: cl, interval: cfg.MinRequestInterval}
	}
	pageSize := cfg.pageSize
	if pageSize <= 0 {
		pageSize = candlePageSize
	}
	return &Client{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		credential:  cfg.Credential,
		http:        httpClient,
		clock:       cl,
		limiter:     limiter,
		maxAttempts: maxAttempts,
		retryBase:   retryBase,
		pageSize:    pageSize,
	}, nil
}

// CandleRequest describes one candle download, in archive-native terms:
// Symbol is the archive/store form (six uppercase letters, no
// underscore — "EURUSD"), converted to OANDA's own wire form
// ("EUR_USD") internally; From/To is the half-open range requested.
type CandleRequest struct {
	Symbol   string
	Interval RawInterval
	From, To time.Time
}

// FetchCandles downloads every candle in req's range, paginating
// automatically (OANDA caps one response at 5000 candles) and pacing/
// retrying per Client's configured policy. Returned Records are in
// ascending Time order, exactly as OANDA returns them, with prices
// parsed through num.ParsePrice — never float64 (ADR-004).
//
// FetchCandles honors ctx cancellation between pages and within a
// single attempt's retry backoff; it starts no goroutines and performs
// no background work, returning once the range is exhausted, an attempt
// permanently fails, or ctx is done.
func (c *Client) FetchCandles(ctx context.Context, req CandleRequest) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	apiSymbol, err := toAPISymbol(req.Symbol)
	if err != nil {
		return nil, fmt.Errorf("oanda: fetch candles: %w", err)
	}
	granularity, err := toGranularity(req.Interval)
	if err != nil {
		return nil, fmt.Errorf("oanda: fetch candles: %w", err)
	}
	if !req.To.After(req.From) {
		return nil, fmt.Errorf("oanda: fetch candles: %w: to must be after from", ErrBadRequest)
	}

	var out []Record
	cursor := req.From.UTC()
	end := req.To.UTC()

	for cursor.Before(end) {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		page, err := c.fetchPage(ctx, apiSymbol, granularity, cursor)
		if err != nil {
			return out, err
		}
		if len(page) == 0 {
			break
		}

		var lastTime time.Time
		for _, r := range page {
			if !r.Time.Before(end) {
				return out, nil
			}
			out = append(out, r)
			lastTime = r.Time
		}
		if lastTime.IsZero() {
			break
		}
		if len(page) < c.pageSize {
			break
		}
		cursor = lastTime.Add(time.Nanosecond)
	}
	return out, nil
}

// candlePageSize is the real OANDA API's maximum candles per response.
// Client.pageSize defaults to this; tests may override it (via
// ClientConfig's unexported pageSize) to exercise pagination without
// constructing thousands of fixture candles.
const candlePageSize = 5000

// fetchPage issues one paginated candles request starting at from,
// retrying transient failures per Client's policy.
func (c *Client) fetchPage(ctx context.Context, apiSymbol, granularity string, from time.Time) ([]Record, error) {
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if attempt > 1 {
			delay := c.retryBase * time.Duration(1<<uint(attempt-2))
			timer := c.clock.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C():
			}
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		records, err := c.doFetchPage(ctx, apiSymbol, granularity, from)
		if err == nil {
			return records, nil
		}
		if !isTransient(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("oanda: %w after %d attempts: %v", ErrRetriesExhausted, c.maxAttempts, lastErr)
}

// isTransient reports whether err (as classified by classifyStatus, or a
// plain network-level error from HTTPDoer.Do) should be retried.
func isTransient(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, errNetwork)
}

// errNetwork marks a failure from HTTPDoer.Do itself (no HTTP response
// at all — DNS, connection refused, timeout), always treated as
// transient.
var errNetwork = errors.New("oanda: network error")

// doFetchPage issues exactly one HTTP request and parses its response.
// It never retries; fetchPage owns retry policy.
func (c *Client) doFetchPage(ctx context.Context, apiSymbol, granularity string, from time.Time) ([]Record, error) {
	token, err := c.credential.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("oanda: resolve credential: %w", err)
	}

	u, err := url.Parse(c.baseURL + "/v3/instruments/" + apiSymbol + "/candles")
	if err != nil {
		return nil, fmt.Errorf("oanda: %w: %v", ErrBadRequest, err)
	}
	q := u.Query()
	q.Set("granularity", granularity)
	q.Set("price", "BA")
	q.Set("count", strconv.Itoa(c.pageSize))
	q.Set("from", from.Format(time.RFC3339Nano))
	q.Set("dailyAlignment", dailyAlignment)
	q.Set("alignmentTimezone", alignmentTimezone)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w: %v", ErrBadRequest, err)
	}
	// Never logged, never included in an error message: the token is
	// only ever placed on this one outgoing request header.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errNetwork, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, classifyStatus(resp.StatusCode, resp.Body)
	}

	var parsed candleResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("oanda: %w: decode response: %v", ErrBadRequest, err)
	}
	return parsed.records()
}

// classifyStatus maps an HTTP status code to one of Client's sentinel
// errors, including a body excerpt for diagnostics — but never any
// request header, so the bearer token can never leak into an error
// message via this path (OANDA does not echo request headers in error
// bodies either, but the exclusion is structural here, not incidental).
func classifyStatus(status int, body io.Reader) error {
	b, _ := io.ReadAll(io.LimitReader(body, 4*1024))
	excerpt := strings.TrimSpace(string(b))
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("%w: http %d: %s", ErrUnauthorized, status, excerpt)
	case status == http.StatusBadRequest || status == http.StatusNotFound:
		return fmt.Errorf("%w: http %d: %s", ErrBadRequest, status, excerpt)
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("%w: http %d: %s", ErrRateLimited, status, excerpt)
	case status >= 500:
		return fmt.Errorf("%w: http %d: %s", ErrProviderUnavailable, status, excerpt)
	default:
		return fmt.Errorf("%w: http %d: %s", ErrUnexpectedStatus, status, excerpt)
	}
}

// candleResponse is the JSON shape OANDA's /v3/instruments/{i}/candles
// endpoint returns for price=BA (bid and ask OHLC sets together).
type candleResponse struct {
	Candles []struct {
		Complete bool   `json:"complete"`
		Time     string `json:"time"`
		Volume   int64  `json:"volume"`
		Bid      *ohlc  `json:"bid"`
		Ask      *ohlc  `json:"ask"`
	} `json:"candles"`
}

type ohlc struct {
	O string `json:"o"`
	H string `json:"h"`
	L string `json:"l"`
	C string `json:"c"`
}

// records converts the decoded response into Records, parsing every
// price through num.ParsePrice and every timestamp through time.Parse —
// never float64 or position-derived, matching ADR-004/#75's existing
// discipline for this package.
func (r candleResponse) records() ([]Record, error) {
	out := make([]Record, 0, len(r.Candles))
	for i, c := range r.Candles {
		if c.Bid == nil || c.Ask == nil {
			return nil, fmt.Errorf("oanda: %w: candle %d missing bid/ask", ErrBadRequest, i)
		}
		t, err := time.Parse(time.RFC3339Nano, c.Time)
		if err != nil {
			return nil, fmt.Errorf("oanda: %w: candle %d: time: %v", ErrBadRequest, i, err)
		}
		rec := Record{Time: t.UTC(), Volume: c.Volume, Complete: c.Complete}
		prices := []struct {
			dst  *num.Price
			s    string
			name string
		}{
			{&rec.BidOpen, c.Bid.O, "bid.o"}, {&rec.BidHigh, c.Bid.H, "bid.h"},
			{&rec.BidLow, c.Bid.L, "bid.l"}, {&rec.BidClose, c.Bid.C, "bid.c"},
			{&rec.AskOpen, c.Ask.O, "ask.o"}, {&rec.AskHigh, c.Ask.H, "ask.h"},
			{&rec.AskLow, c.Ask.L, "ask.l"}, {&rec.AskClose, c.Ask.C, "ask.c"},
		}
		for _, p := range prices {
			v, err := num.ParsePrice(p.s)
			if err != nil {
				return nil, fmt.Errorf("oanda: %w: candle %d: %s: %v", ErrBadRequest, i, p.name, err)
			}
			*p.dst = v
		}
		out = append(out, rec)
	}
	return out, nil
}

// toAPISymbol converts an archive-form symbol ("EURUSD") to OANDA's own
// wire form ("EUR_USD"). It reuses resolveSymbol's scope validation
// (six uppercase letters, one of the 24 in-scope pairs) so an
// out-of-scope or malformed symbol is rejected before ever reaching a
// request — XAUUSD, in particular, can never be requested through this
// client, matching the issue's explicit constraint.
func toAPISymbol(symbol string) (string, error) {
	if _, err := resolveSymbol(symbol); err != nil {
		return "", err
	}
	return symbol[:3] + "_" + symbol[3:], nil
}

// toGranularity maps a RawInterval to OANDA's own granularity token.
// OANDA spells daily "D", not "D1".
func toGranularity(interval RawInterval) (string, error) {
	switch interval {
	case RawM1:
		return "M1", nil
	case RawH1:
		return "H1", nil
	case RawH4:
		return "H4", nil
	case RawD1:
		return "D", nil
	default:
		return "", fmt.Errorf("%w: unsupported interval %q", ErrUnsupportedInterval, interval)
	}
}
