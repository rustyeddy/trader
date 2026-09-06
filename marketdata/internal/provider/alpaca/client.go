package alpaca

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
	"sync"
	"time"

	"github.com/rustyeddy/trader/clock"
)

// Sentinel errors Client returns (wrapped), classifying a failed
// request without requiring a caller to inspect an HTTP status code
// directly — mirrors oanda.Client's identical classification shape and
// retry policy (issue #297). A permanent error (ErrUnauthorized,
// ErrBadRequest) is never retried; a transient one (ErrRateLimited,
// ErrProviderUnavailable, or a network-level failure) is retried up to
// Client's configured attempt limit, reported as ErrRetriesExhausted
// if every attempt fails.
var (
	// ErrUnauthorized marks an HTTP 401/403 response: the credential
	// pair is missing, invalid, or lacks access. Retrying with the same
	// credentials cannot succeed.
	ErrUnauthorized = errors.New("alpaca: unauthorized")
	// ErrBadRequest marks an HTTP 400/404 response: the request itself
	// (symbol, timeframe, range) is malformed or refers to something
	// Alpaca does not recognize. Retrying without changing the request
	// cannot succeed.
	ErrBadRequest = errors.New("alpaca: bad request")
	// ErrRateLimited marks an HTTP 429 response.
	ErrRateLimited = errors.New("alpaca: rate limited")
	// ErrProviderUnavailable marks an HTTP 5xx response.
	ErrProviderUnavailable = errors.New("alpaca: provider unavailable")
	// ErrRetriesExhausted marks a request that failed transiently on
	// every attempt Client's retry policy allowed.
	ErrRetriesExhausted = errors.New("alpaca: retries exhausted")
	// ErrUnexpectedStatus marks any other non-2xx status. Treated as
	// permanent (not retried).
	ErrUnexpectedStatus = errors.New("alpaca: unexpected status")
)

// HTTPDoer is the minimal seam Client issues requests through —
// oanda.HTTPDoer's identical shape. Satisfied by *http.Client; tests
// inject a fake implementation, so no unit test in this package ever
// makes a real network call.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// rateLimiter and its two implementations are oanda.Client's identical
// pacing mechanism, copied verbatim in shape (not import — this
// package must not depend on oanda) since a live-API provider's
// pacing/backoff needs are provider-independent.
type rateLimiter interface {
	Wait(ctx context.Context) error
}

type noopRateLimiter struct{}

func (noopRateLimiter) Wait(context.Context) error { return nil }

type fixedIntervalLimiter struct {
	clock    clock.Clock
	interval time.Duration

	mu   sync.Mutex
	last time.Time
}

func (l *fixedIntervalLimiter) Wait(ctx context.Context) error {
	if l.interval <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

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

// ClientConfig holds Client's explicit dependencies. Configuration is
// a composition-root concern, the same convention oanda.ClientConfig/
// marketdata.Manager follow: Client never reads environment variables
// or files itself.
type ClientConfig struct {
	// BaseURL is Alpaca's Market Data API base, for example
	// "https://data.alpaca.markets". Required. A full URL, not an
	// enum Client would parse itself — oanda.ClientConfig.BaseURL's
	// identical reasoning.
	BaseURL string
	// Credential supplies the key ID/secret key pair for every
	// request. Required.
	Credential CredentialProvider

	// MaxAttempts bounds how many times a transiently-failing request
	// is attempted in total. Non-positive selects a package default (3).
	MaxAttempts int
	// RetryBaseDelay is the delay before the first retry; each
	// subsequent retry doubles it. Non-positive selects a package
	// default (500ms).
	RetryBaseDelay time.Duration
	// MinRequestInterval enforces a minimum spacing between requests
	// when positive. Zero (the default) disables rate limiting at this
	// layer.
	MinRequestInterval time.Duration

	// HTTPClient overrides the transport Client issues requests through
	// (default http.DefaultClient).
	HTTPClient HTTPDoer

	// clock and pageLimit are internal test seams, unexported so no
	// caller can inject a non-deterministic dependency or a
	// non-provider-accurate page size through Config.
	clock     clock.Clock
	pageLimit int
}

const (
	defaultMaxAttempts    = 3
	defaultRetryBaseDelay = 500 * time.Millisecond
)

// Client is a minimal Alpaca Market Data API v2 client for historical
// daily-bar download (issue #297). It is the only network-facing type
// in this package. Client is safe for concurrent use: FetchBars may be
// called from multiple goroutines against the same Client, and the
// rate limiter serializes and paces their requests against each other.
type Client struct {
	baseURL     string
	credential  CredentialProvider
	http        HTTPDoer
	clock       clock.Clock
	limiter     rateLimiter
	maxAttempts int
	retryBase   time.Duration
	pageLimit   int
}

// NewClient validates cfg and returns a ready-to-use Client.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("alpaca: new client: base URL is required")
	}
	if cfg.Credential == nil {
		return nil, fmt.Errorf("alpaca: new client: credential is required")
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
	pageLimit := cfg.pageLimit
	if pageLimit <= 0 {
		pageLimit = defaultPageLimit
	}
	return &Client{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		credential:  cfg.Credential,
		http:        httpClient,
		clock:       cl,
		limiter:     limiter,
		maxAttempts: maxAttempts,
		retryBase:   retryBase,
		pageLimit:   pageLimit,
	}, nil
}

// defaultPageLimit is the page size this client requests. Alpaca's
// real API (per the unverified assumption documented in doc.go) caps
// a single response at 10,000 bars; this client requests a smaller,
// still-generous default so an ordinary Phase 1 daily-bar fetch (at
// most a few thousand rows for a multi-year range) rarely if ever
// needs more than one page in practice, while pagination itself is
// still fully implemented and tested for the case where the API
// enforces its own smaller cap or a longer range is requested.
const defaultPageLimit = 10000

// BarRequest describes one historical daily-bar download: Symbol is
// Alpaca's own bare-ticker wire form (no suffix, unlike Stooq's
// "spy.us" — for example "SPY"); From/To is the half-open range
// requested, in UTC.
type BarRequest struct {
	Symbol   string
	From, To time.Time
}

// FetchBars downloads every daily bar in req's range, paginating
// automatically via the assumed next_page_token cursor (see doc.go's
// unverified-API-shape note) and pacing/retrying per Client's
// configured policy. Returned Records are in ascending Time order,
// with Time already re-anchored to midnight UTC of each bar's own
// trading date (see recordFromWireBar in wireshape.go) — not the
// literal fetched instant.
//
// FetchBars honors ctx cancellation between pages and within a single
// attempt's retry backoff; it starts no goroutines and performs no
// background work.
func (c *Client) FetchBars(ctx context.Context, req BarRequest) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if symbol == "" {
		return nil, fmt.Errorf("alpaca: fetch bars: %w: symbol is required", ErrBadRequest)
	}
	if !req.To.After(req.From) {
		return nil, fmt.Errorf("alpaca: fetch bars: %w: to must be after from", ErrBadRequest)
	}

	var out []Record
	pageToken := ""
	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		bars, next, err := c.fetchPage(ctx, symbol, req.From, req.To, pageToken)
		if err != nil {
			return out, err
		}
		out = append(out, bars...)
		if next == "" {
			break
		}
		pageToken = next
	}
	return out, nil
}

// fetchPage issues one paginated bars request, retrying transient
// failures per Client's policy, and returns the page's records plus
// Alpaca's own next-page cursor (empty when this was the last page).
func (c *Client) fetchPage(ctx context.Context, symbol string, from, to time.Time, pageToken string) ([]Record, string, error) {
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if attempt > 1 {
			delay := c.retryBase * time.Duration(1<<uint(attempt-2))
			timer := c.clock.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, "", ctx.Err()
			case <-timer.C():
			}
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, "", err
		}

		bars, next, err := c.doFetchPage(ctx, symbol, from, to, pageToken)
		if err == nil {
			return bars, next, nil
		}
		if !isTransient(err) {
			return nil, "", err
		}
		lastErr = err
	}
	return nil, "", fmt.Errorf("alpaca: %w after %d attempts: %v", ErrRetriesExhausted, c.maxAttempts, lastErr)
}

// isTransient reports whether err should be retried.
func isTransient(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, errNetwork)
}

// errNetwork marks a failure from HTTPDoer.Do itself (no HTTP response
// at all), always treated as transient.
var errNetwork = errors.New("alpaca: network error")

// doFetchPage issues exactly one HTTP request and parses its response.
// It never retries; fetchPage owns retry policy.
//
// # Request shape (unverified — see doc.go)
//
// GET {baseURL}/v2/stocks/{symbol}/bars, with query parameters
// timeframe=1Day, start/end (RFC3339), limit, adjustment=split (see
// ADR-050 for why "split," matching Stooq's own AdjustmentSplitAdjusted
// choice, was picked over "raw"/"dividend"/"all"), feed=iex (Phase 1
// deliberately pins the free-tier IEX feed rather than exposing feed
// selection — see the package doc comment; IEX is one specific
// exchange's own data, not the consolidated multi-exchange SIP tape,
// so OHLC/volume values from this provider are not directly comparable
// to a consolidated-feed source for the same symbol/date — PR #312
// review), and page_token when
// continuing a prior page. Auth is two headers, APCA-API-KEY-ID and
// APCA-API-SECRET-KEY — never a single bearer token the way OANDA
// uses.
func (c *Client) doFetchPage(ctx context.Context, symbol string, from, to time.Time, pageToken string) ([]Record, string, error) {
	keyID, secretKey, err := c.credential.Credentials(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("alpaca: resolve credentials: %w", err)
	}

	u, err := url.Parse(c.baseURL + "/v2/stocks/" + symbol + "/bars")
	if err != nil {
		return nil, "", fmt.Errorf("alpaca: %w: %v", ErrBadRequest, err)
	}
	q := u.Query()
	q.Set("timeframe", "1Day")
	q.Set("start", from.UTC().Format(time.RFC3339Nano))
	q.Set("end", to.UTC().Format(time.RFC3339Nano))
	q.Set("limit", strconv.Itoa(c.pageLimit))
	q.Set("adjustment", "split")
	q.Set("feed", "iex")
	if pageToken != "" {
		q.Set("page_token", pageToken)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("alpaca: %w: %v", ErrBadRequest, err)
	}
	// Never logged, never included in an error message: both secrets
	// are only ever placed on these two outgoing request headers.
	req.Header.Set("APCA-API-KEY-ID", keyID)
	req.Header.Set("APCA-API-SECRET-KEY", secretKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", errNetwork, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", classifyStatus(resp.StatusCode, resp.Body)
	}

	var parsed barsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, "", fmt.Errorf("alpaca: %w: decode response: %v", ErrBadRequest, err)
	}
	records, err := parsed.records()
	if err != nil {
		return nil, "", err
	}
	return records, parsed.NextPageToken, nil
}

// classifyStatus maps an HTTP status code to one of Client's sentinel
// errors, including a body excerpt for diagnostics — never a request
// header, so neither secret can leak into an error message via this
// path.
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
