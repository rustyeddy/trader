package oanda

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rustyeddy/trader/internal/logging"
	"github.com/rustyeddy/trader/num"
)

// Sentinel errors Client returns (wrapped), classifying a failed request
// without requiring a caller to inspect an HTTP status code directly
// (issue #80). A permanent error (ErrBadRequest, a 403, an empty token) is
// never retried; a transient one (ErrRateLimited, ErrProviderUnavailable,
// a network-level failure, or an HTTP 401) is retried up to the Client's
// configured attempt limit, reported as ErrRetriesExhausted if every attempt
// fails. A 401 is retried because OANDA has answered a valid token with a
// one-off 401 while the same token succeeded on the next request (#624); when
// every attempt is refused the error is both ErrRetriesExhausted and
// ErrUnauthorized, so a bad token still fails clearly.
var (
	// ErrUnauthorized marks an HTTP 401/403 response: the credential is
	// missing, invalid, or lacks access. Retrying with the same
	// credential cannot succeed, except that a lone 401 is retried (see above).
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
// coarser request rather than left to OANDA's account defaults (ADR-020): the
// preserved raw archive's D1 opens only happen to match a 17:00
// America/New_York rollover because the legacy client inherited that as an
// account default, not because it requested it. Pinning them explicitly means a
// future account-default change cannot silently misalign new data against the
// existing archive.
const (
	dailyAlignment    = "17"
	alignmentTimezone = "America/New_York"
)

// maxResponseBytes bounds how much of a response body the client reads. A full
// page of 5000 bid/ask candles is about 2 MB.
const maxResponseBytes = 16 << 20

// maxBackoffShift caps the retry delay's doubling so a large MaxAttempts cannot
// overflow or wait unreasonably long.
const maxBackoffShift = 6

// HTTPDoer is the minimal seam Client issues requests through. It is satisfied
// by *http.Client; tests inject a fake, so no unit test makes a real network
// call.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Sleeper waits for d or until ctx is done, returning ctx's error in that case.
// It is how Client waits between retries and between paced requests; tests
// inject one that records the delay and returns at once, so they never wait.
type Sleeper func(ctx context.Context, d time.Duration) error

// realSleep is the production Sleeper.
func realSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// paceLimiter enforces a minimum spacing between requests. It reads time
// through now and waits through sleep, so a test drives it with no real delay.
// mu serializes both the read and write of last and the wait itself, so
// concurrent callers are paced against each other.
type paceLimiter struct {
	now      func() time.Time
	sleep    Sleeper
	interval time.Duration

	mu   sync.Mutex
	last time.Time
}

func (l *paceLimiter) wait(ctx context.Context) error {
	if l == nil || l.interval <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if !l.last.IsZero() {
		if d := l.interval - now.Sub(l.last); d > 0 {
			if err := l.sleep(ctx, d); err != nil {
				return err
			}
			now = l.now()
		}
	}
	l.last = now
	return nil
}

// normalizeBaseURL trims trailing slashes and one trailing "/v3" path segment
// from base, since Client adds the version itself. It works on the parsed URL's
// path, never the raw string, so a host that happens to end in "v3" is left alone.
func normalizeBaseURL(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" {
		return strings.TrimRight(base, "/")
	}
	p := strings.TrimRight(u.Path, "/")
	p = strings.TrimSuffix(p, "/v3")
	u.Path, u.RawPath = strings.TrimRight(p, "/"), ""
	return strings.TrimRight(u.String(), "/")
}

// noRedirectClient is the default transport: a redirect is returned as the
// response (a non-200 status, so an error) instead of being followed.
func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// checkBaseURL requires an absolute https URL with no credentials, so the
// bearer token is never sent in clear text or leaked through userinfo. Plain
// http is allowed only to a loopback host, which is how tests use httptest.
func checkBaseURL(base string) error {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" {
		return errors.New("base URL is not an absolute URL")
	}
	if u.User != nil {
		return errors.New("base URL must not carry credentials")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return errors.New("base URL must be https (http is allowed only for a loopback host)")
	default:
		return errors.New("base URL must be https")
	}
}

// ClientConfig holds Client's explicit dependencies. Configuration is a
// composition-root concern: Client never reads environment variables or files.
type ClientConfig struct {
	// BaseURL is OANDA's API base, for example
	// "https://api-fxpractice.oanda.com", with or without OANDA's trailing
	// "/v3" (Client adds the version itself). Required, absolute, https.
	// Environment selection is the composition root's decision, not parsed here.
	BaseURL string
	// Credential supplies the bearer token for every request. Required.
	Credential CredentialProvider
	// Logger receives structured records of requests, retries and pages. Nil
	// means silent. It never receives the token, a request header or a
	// response body.
	Logger *slog.Logger

	// MaxAttempts bounds how many times a transiently-failing request is
	// attempted in total (the first try plus retries). Non-positive selects 3.
	MaxAttempts int
	// RetryBaseDelay is the delay before the first retry; each later retry
	// doubles it (capped). Non-positive selects 500ms.
	RetryBaseDelay time.Duration
	// MinRequestInterval enforces a minimum spacing between requests when
	// positive. Zero disables pacing at this layer.
	MinRequestInterval time.Duration

	// HTTPClient overrides the transport. The default does not follow
	// redirects, so the bearer token can never be forwarded to another URL; a
	// replacement must do the same, since a client that follows a redirect to
	// plain http would send the token in clear text.
	HTTPClient HTTPDoer
	// Sleep waits between retries and paced requests (default: a real timer
	// that honors ctx). Tests inject one that does not wait.
	Sleep Sleeper
	// Now reports the time pacing is measured against (default time.Now).
	Now func() time.Time

	// pageSize is an internal test seam, so tests can exercise a page boundary
	// without thousands of fixture candles.
	pageSize int
}

const (
	defaultMaxAttempts    = 3
	defaultRetryBaseDelay = 500 * time.Millisecond
)

// Client is a minimal OANDA v20 REST client for historical candle download
// (issue #80). It is the only network-facing type in this package. Client is
// safe for concurrent use: the pacing limiter serializes concurrent requests.
type Client struct {
	baseURL     string
	credential  CredentialProvider
	http        HTTPDoer
	log         *slog.Logger
	sleep       Sleeper
	limiter     *paceLimiter
	maxAttempts int
	retryBase   time.Duration
	pageSize    int
}

// NewClient validates cfg and returns a ready-to-use Client.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("oanda: new client: base URL is required")
	}
	if err := checkBaseURL(cfg.BaseURL); err != nil {
		return nil, fmt.Errorf("oanda: new client: %w", err)
	}
	if cfg.Credential == nil {
		return nil, fmt.Errorf("oanda: new client: credential is required")
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = realSleep
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = noRedirectClient()
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	retryBase := cfg.RetryBaseDelay
	if retryBase <= 0 {
		retryBase = defaultRetryBaseDelay
	}
	var limiter *paceLimiter
	if cfg.MinRequestInterval > 0 {
		limiter = &paceLimiter{now: now, sleep: sleep, interval: cfg.MinRequestInterval}
	}
	pageSize := cfg.pageSize
	if pageSize <= 0 {
		pageSize = candlePageSize
	}
	log := cfg.Logger
	if log == nil {
		log = logging.Discard()
	}
	return &Client{
		baseURL:     normalizeBaseURL(cfg.BaseURL),
		credential:  cfg.Credential,
		http:        httpClient,
		log:         logging.WithComponent(log, logging.ComponentMarketData).With("provider", source),
		sleep:       sleep,
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

// FetchCandles downloads every candle in req's range, paginating automatically
// (OANDA caps one response at 5000 candles) and pacing and retrying per the
// Client's policy. Returned Records are in ascending Time order with prices
// parsed through num.ParsePrice, never float64 (ADR-004). The provider's last
// candle may be incomplete (Complete false); it is returned as the provider
// declares it, and what to do with it is the caller's decision.
//
// A page whose candles are out of order or repeat the previous page's last
// candle is ErrMalformedData, never silently accepted, which also guarantees
// pagination makes progress. Later pages continue from the last candle with
// includeFirst=false; the candle covering an unaligned From, which opened
// before it, is dropped. A 200 response without a candles array is
// ErrMalformedData, so a truncated body never looks like an empty range. FetchCandles honors ctx cancellation between pages
// and during retry backoff; it starts no goroutines. On error the records
// fetched so far are returned with it.
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

	log := c.log.With("symbol", req.Symbol, "interval", string(req.Interval))
	log.Info("fetch started", "from", req.From.UTC(), "to", req.To.UTC())

	var out []Record
	start := req.From.UTC()
	cursor := start
	includeFirst := true // the first page includes a candle that opens exactly at from
	end := req.To.UTC()

	for pageNo := 1; cursor.Before(end); pageNo++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		page, err := c.fetchPage(ctx, log, apiSymbol, granularity, cursor, includeFirst)
		if err != nil {
			log.Error("fetch failed", "page", pageNo, "records", len(out), "error", err)
			return out, err
		}
		full := len(page) >= c.pageSize
		if pageNo == 1 {
			// OANDA also returns the candle that covers an unaligned from,
			// which opened before it; it is not part of the requested range.
			for len(page) > 0 && page[0].Time.Before(start) {
				page = page[1:]
			}
		}
		if len(page) == 0 {
			break
		}
		var last time.Time
		if len(out) > 0 {
			last = out[len(out)-1].Time
		}
		if err := checkPage(page, last); err != nil {
			log.Error("fetch failed", "page", pageNo, "records", len(out), "error", err)
			return out, err
		}

		reachedEnd := false
		for _, r := range page {
			if !r.Time.Before(end) {
				reachedEnd = true
				break
			}
			out = append(out, r)
		}
		log.Debug("page fetched", "page", pageNo, "page_records", len(page), "records", len(out))
		if reachedEnd || !full {
			break
		}
		// Continue from the last candle itself and ask the provider to leave it
		// out, rather than guess the next opening time.
		cursor = page[len(page)-1].Time
		includeFirst = false
	}
	log.Info("fetch finished", "records", len(out))
	return out, nil
}

// checkPage requires strictly ascending times, the first after last (the last
// record already collected; the zero time for the first page), so pagination
// always makes progress and no candle is repeated.
func checkPage(page []Record, last time.Time) error {
	prev := last
	for i, r := range page {
		if !prev.IsZero() && !r.Time.After(prev) {
			return fmt.Errorf("oanda: %w: candle %d at %s is not after %s",
				ErrMalformedData, i, r.Time.Format(time.RFC3339Nano), prev.Format(time.RFC3339Nano))
		}
		prev = r.Time
	}
	return nil
}

// candlePageSize is the real OANDA API's maximum candles per response.
const candlePageSize = 5000

// fetchPage issues one paginated candles request starting at from, retrying
// transient failures per the Client's policy.
func (c *Client) fetchPage(ctx context.Context, log *slog.Logger, apiSymbol, granularity string, from time.Time, includeFirst bool) ([]Record, error) {
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if attempt > 1 {
			delay := c.retryBase << min(attempt-2, maxBackoffShift)
			log.Warn("retrying", "attempt", attempt, "of", c.maxAttempts, "delay", delay, "error", lastErr)
			if err := c.sleep(ctx, delay); err != nil {
				return nil, err
			}
		}
		if err := c.limiter.wait(ctx); err != nil {
			return nil, err
		}

		records, err := c.doFetchPage(ctx, log, apiSymbol, granularity, from, includeFirst, attempt)
		if err == nil {
			return records, nil
		}
		if !isTransient(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("oanda: %w after %d attempts: %w", ErrRetriesExhausted, c.maxAttempts, lastErr)
}

// isTransient reports whether err should be retried.
func isTransient(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, errNetwork) || errors.Is(err, err401)
}

// err401 marks an HTTP 401 specifically (a 403 is not retried), so the retry
// policy can tell them apart; it always accompanies ErrUnauthorized.
var err401 = errors.New("oanda: http 401")

// errNetwork marks a failure from HTTPDoer.Do itself (no HTTP response at all:
// DNS, connection refused, timeout), always treated as transient.
var errNetwork = errors.New("oanda: network error")

// doFetchPage issues exactly one HTTP request and parses its response. It never
// retries; fetchPage owns retry policy. Every error it returns has had the
// token scrubbed from its text.
func (c *Client) doFetchPage(ctx context.Context, log *slog.Logger, apiSymbol, granularity string, from time.Time, includeFirst bool, attempt int) ([]Record, error) {
	token, err := c.credential.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("oanda: resolve credential: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("oanda: %w: the credential provider returned an empty token", ErrUnauthorized)
	}
	records, err := c.doRequest(ctx, log, token, apiSymbol, granularity, from, includeFirst, attempt)
	if err != nil {
		return nil, scrubError(err, token)
	}
	return records, nil
}

func (c *Client) doRequest(ctx context.Context, log *slog.Logger, token, apiSymbol, granularity string, from time.Time, includeFirst bool, attempt int) ([]Record, error) {
	u, err := url.Parse(c.baseURL + "/v3/instruments/" + apiSymbol + "/candles")
	if err != nil {
		return nil, fmt.Errorf("oanda: %w: %v", ErrBadRequest, err)
	}
	q := u.Query()
	q.Set("granularity", granularity)
	q.Set("price", "BA")
	q.Set("count", strconv.Itoa(c.pageSize))
	q.Set("from", from.Format(time.RFC3339Nano))
	q.Set("includeFirst", strconv.FormatBool(includeFirst))
	q.Set("dailyAlignment", dailyAlignment)
	q.Set("alignmentTimezone", alignmentTimezone)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w: %v", ErrBadRequest, err)
	}
	// The token is placed only on this one outgoing header: never logged, never
	// in an error message.
	req.Header.Set("Authorization", "Bearer "+token)

	log.Debug("request", "attempt", attempt, "from", from)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%w: %v", errNetwork, err)
	}
	defer func() { _ = resp.Body.Close() }()
	log.Debug("response", "attempt", attempt, "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, classifyStatus(resp.StatusCode, resp.Body)
	}

	// Read the whole body under the limit first, so the size rule covers any
	// trailing data too, then require exactly one JSON value.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%w: read response: %v", errNetwork, err)
	}
	if len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("oanda: %w: response exceeds %d bytes", ErrMalformedData, maxResponseBytes)
	}
	var parsed candleResponse
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("oanda: %w: decode response: %v", ErrMalformedData, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("oanda: %w: unexpected data after the response", ErrMalformedData)
	}
	return parsed.records()
}

// scrubError returns err unchanged unless its text contains token, in which case
// it returns an error with the same classification (it still matches the same
// errors.Is sentinels) and the token replaced by [redacted]. This is a backstop:
// the client never puts the token into an error, but a transport or a provider
// error body could echo it.
func scrubError(err error, token string) error {
	if token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return &scrubbed{msg: strings.ReplaceAll(err.Error(), token, "[redacted]"), cause: err}
}

// scrubbed carries a redacted message but unwraps to the original chain's
// classification only, never exposing the original text.
type scrubbed struct {
	msg   string
	cause error
}

func (e *scrubbed) Error() string { return e.msg }

// Is reports whether the original error matches target, so errors.Is still
// classifies a scrubbed error.
func (e *scrubbed) Is(target error) bool { return errors.Is(e.cause, target) }

// classifyStatus maps an HTTP status code to one of Client's sentinel errors,
// including a body excerpt for diagnostics but never any request header.
func classifyStatus(status int, body io.Reader) error {
	b, _ := io.ReadAll(io.LimitReader(body, 4*1024))
	excerpt := strings.TrimSpace(string(b))
	switch {
	case status == http.StatusUnauthorized:
		return fmt.Errorf("%w: %w: http %d: %s", ErrUnauthorized, err401, status, excerpt)
	case status == http.StatusForbidden:
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
	Candles *[]struct {
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
	if r.Candles == nil {
		return nil, fmt.Errorf("oanda: %w: response has no candles array", ErrMalformedData)
	}
	out := make([]Record, 0, len(*r.Candles))
	for i, c := range *r.Candles {
		if c.Bid == nil || c.Ask == nil {
			return nil, fmt.Errorf("oanda: %w: candle %d missing bid/ask", ErrMalformedData, i)
		}
		t, err := time.Parse(time.RFC3339Nano, c.Time)
		if err != nil {
			return nil, fmt.Errorf("oanda: %w: candle %d: time: %v", ErrMalformedData, i, err)
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
				return nil, fmt.Errorf("oanda: %w: candle %d: %s: %v", ErrMalformedData, i, p.name, err)
			}
			*p.dst = v
		}
		out = append(out, rec)
	}
	return out, nil
}

// toAPISymbol converts an archive-form symbol ("EURUSD") to OANDA's own
// wire form ("EUR_USD"). It reuses resolveSymbol's scope validation
// (six uppercase letters, one of the 26 in-scope pairs) so an
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
