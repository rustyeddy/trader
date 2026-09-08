package alpaca

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rustyeddy/trader/clock"

	sdkalpaca "github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	sdkmarketdata "github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/mailru/easyjson/jlexer"
)

// Sentinel errors Client returns (wrapped), classifying a failed
// request without requiring a caller to inspect an HTTP status code
// directly — mirrors oanda.Client's identical classification shape and
// retry policy (issue #297, carried forward unchanged by issue #323's
// SDK migration). A permanent error (ErrUnauthorized, ErrBadRequest)
// is never retried; a transient one (ErrRateLimited,
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
	// cannot succeed. Also used for a response this package cannot
	// decode at all (see classifySDKErr) — a permanent, not transient,
	// condition.
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

// Feed selects which historical data feed Alpaca serves a request
// from. Phase 1 pins FeedIEX as ClientConfig's default (ADR-050's own
// free-tier choice); FeedSIP exists so a caller with a paid SIP
// entitlement may select it explicitly. This package never infers a
// feed from account tier — issue #323's own explicit requirement.
type Feed string

const (
	// FeedIEX is the Investors Exchange feed: one specific exchange's
	// own data, not the consolidated multi-exchange tape. Available on
	// Alpaca's free tier. ClientConfig's default.
	FeedIEX Feed = "iex"
	// FeedSIP is the consolidated multi-exchange Securities Information
	// Processor tape. Requires a paid Alpaca market-data entitlement;
	// selecting it does not itself grant access — Alpaca's API rejects
	// an unentitled request with ErrUnauthorized, the same as any other
	// authorization failure.
	FeedSIP Feed = "sip"
)

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
	// Feed selects the historical data feed. Zero value selects
	// FeedIEX (ADR-050's pinned Phase 1 default).
	Feed Feed

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
	// RequestTimeout bounds a single underlying HTTP request. The
	// official Alpaca Go SDK's historical-bars methods accept no
	// context.Context (see Client's own doc comment on cancellation),
	// so ctx cancellation cannot interrupt a request already in
	// flight — RequestTimeout is the only backstop against one hung
	// indefinitely. Non-positive selects a package default (30s).
	RequestTimeout time.Duration

	// HTTPClient overrides the transport the underlying SDK client
	// issues requests through (default: a *http.Client with
	// RequestTimeout applied). Tests inject one with a fake
	// http.RoundTripper as its Transport, never a real network call.
	HTTPClient *http.Client

	// clock and pageLimit are internal test seams, unexported so no
	// caller can inject a non-deterministic dependency or a
	// non-provider-accurate page size through Config.
	clock     clock.Clock
	pageLimit int
}

const (
	defaultMaxAttempts    = 3
	defaultRetryBaseDelay = 500 * time.Millisecond
	defaultRequestTimeout = 30 * time.Second
)

// Client is a minimal Alpaca Market Data API client for historical
// daily-bar download (issue #297), delegating request construction,
// pagination, and response decoding to the official Alpaca Go SDK
// (github.com/alpacahq/alpaca-trade-api-go/v3, issue #323) rather than
// duplicating that protocol work by hand. Client owns only the
// concerns the SDK does not: retry/backoff policy, rate limiting, and
// translating the SDK's own types into this package's Record.
//
// # Cancellation is coarse-grained, not per-request
//
// The SDK's GetBars/GetMultiBars accept no context.Context and fully
// own their own internal multi-page fetch loop, so ctx cancellation
// cannot interrupt a request — or a later page of a paginated fetch —
// already in flight. FetchBars checks ctx before issuing a fetch
// attempt and while waiting out retry backoff/rate-limit pacing
// between attempts, and RequestTimeout bounds how long any single
// underlying HTTP call may hang, but a caller must not expect
// mid-page-boundary cancellation the way a hand-rolled pagination loop
// could offer. This is an accepted, deliberate consequence of
// delegating wire-protocol ownership to the SDK (issue #323's own
// "delegate that protocol work... instead of duplicating it inside
// Trader" goal) — not an oversight.
//
// # Price precision
//
// The SDK's Bar type decodes prices directly into float64 fields, so
// unlike this package's previous hand-written JSON decoding (which
// preserved the API's original decimal text via json.Number), the
// original wire text no longer exists by the time this package
// receives a value. recordsFromSDKBars (wireshape.go) reconstructs
// each float64's shortest round-tripping decimal text before
// constructing num.Price, preserving real sub-cent precision that a
// split-adjusted historical series can legitimately carry — not the
// cent tick size, which is an execution-time concept and would
// silently discard real history if applied during ingestion (PR #327
// review; see quantizedPriceFromFloat's own doc comment).
//
// Client is safe for concurrent use: FetchBars may be called from
// multiple goroutines against the same Client, and the rate limiter
// serializes and paces their requests against each other.
type Client struct {
	baseURL     string
	credential  CredentialProvider
	feed        Feed
	httpClient  *http.Client
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
	feed := cfg.Feed
	if feed == "" {
		feed = FeedIEX
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	retryBase := cfg.RetryBaseDelay
	if retryBase <= 0 {
		retryBase = defaultRetryBaseDelay
	}
	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	// RequestTimeout applies even to a caller-supplied HTTPClient, as
	// long as that client left its own Timeout unset (<=0): an
	// injected *http.Client is cloned (never mutated in place, so a
	// caller holding their own reference to it is unaffected) with
	// requestTimeout applied, unless the caller explicitly configured
	// a positive Timeout of their own, which is respected as-is (PR
	// #327 review — RequestTimeout previously did nothing whenever
	// HTTPClient was non-nil, silently defeating its own documented
	// purpose as the backstop for the SDK's uncancelable requests).
	httpClient := cfg.HTTPClient
	switch {
	case httpClient == nil:
		httpClient = &http.Client{Timeout: requestTimeout}
	case httpClient.Timeout <= 0:
		clone := *httpClient
		clone.Timeout = requestTimeout
		httpClient = &clone
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
		feed:        feed,
		httpClient:  httpClient,
		clock:       cl,
		limiter:     limiter,
		maxAttempts: maxAttempts,
		retryBase:   retryBase,
		pageLimit:   pageLimit,
	}, nil
}

// Feed returns the historical data feed this Client was configured
// with (ClientConfig.Feed, defaulting to FeedIEX). Issue #324 (EQ-11)
// needs this to record which feed produced a raw partition's data as
// part of that partition's own provenance — Feed selection materially
// changes the resulting bars (ADR-050/052), so it must be recorded,
// not merely applied and forgotten.
func (c *Client) Feed() Feed {
	return c.feed
}

// defaultPageLimit is the page size this client requests from the SDK.
// Alpaca's real API caps a single response at 10,000 bars (v2MaxLimit
// in the SDK); this client requests that same generous default so an
// ordinary Phase 1 daily-bar fetch (at most a few thousand rows for a
// multi-year range) rarely if ever needs more than one page in
// practice, while pagination itself remains fully exercised and tested
// for the case where a longer range is requested.
const defaultPageLimit = 10000

// BarRequest describes one historical daily-bar download: Symbol is
// Alpaca's own bare-ticker wire form (no suffix, unlike Stooq's
// "spy.us" — for example "SPY"); From/To is the half-open range
// requested, in UTC.
type BarRequest struct {
	Symbol   string
	From, To time.Time
}

// FetchBars downloads every daily bar in req's range via the SDK's
// GetBars, which owns pagination internally, and retries the whole
// fetch (not individual pages — see Client's own doc comment) per
// Client's configured attempt/backoff/rate-limit policy. Returned
// Records are in ascending Time order, with Time already re-anchored
// to midnight UTC of each bar's own trading date (see
// recordsFromSDKBars in wireshape.go) — not the literal fetched
// instant.
//
// FetchBars honors ctx cancellation before each attempt and during
// retry backoff/rate-limit waits; it starts no goroutines and performs
// no background work. See Client's own doc comment for why this is
// coarser than per-page cancellation.
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

	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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

		records, err := c.fetchAllPages(ctx, symbol, req.From, req.To)
		if err == nil {
			return records, nil
		}
		if !isTransient(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("alpaca: %w after %d attempts: %v", ErrRetriesExhausted, c.maxAttempts, lastErr)
}

// isTransient reports whether err should be retried.
func isTransient(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, errNetwork)
}

// errNetwork marks a failure that never produced a classifiable
// Alpaca API error response (a transport-level failure, or a
// canceled/timed-out request), always treated as transient.
var errNetwork = errors.New("alpaca: network error")

// fetchAllPages issues one full, SDK-owned multi-page bars fetch: it
// resolves credentials once (CredentialProvider's own doc comment
// already documents that resolving fresh per outer attempt, rather
// than once for a Client's whole lifetime, is the point — this is
// coarser than the previous per-HTTP-page resolution, since the SDK
// owns the page loop internally and offers no per-page hook, but still
// re-resolves on every retried attempt), constructs a short-lived SDK
// client with them, and calls GetBars.
//
// ctx bounds only credential resolution (CredentialProvider itself
// accepts ctx and may block, for example on a remote secret store) —
// the SDK's own GetBars call accepts no context.Context at all and
// cannot be interrupted once issued (see Client's own doc comment).
//
// The SDK's own internal retry (ClientOpts.RetryLimit) is disabled
// (-1): Client.FetchBars is the sole owner of retry/backoff policy, so
// a transient failure here is retried by the caller (FetchBars),
// exactly once per outer attempt, not compounded by a second retry
// layer inside the SDK.
func (c *Client) fetchAllPages(ctx context.Context, symbol string, from, to time.Time) ([]Record, error) {
	keyID, secretKey, err := c.credential.Credentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("alpaca: resolve credentials: %w", err)
	}

	sdkClient := sdkmarketdata.NewClient(sdkmarketdata.ClientOpts{
		APIKey:     keyID,
		APISecret:  secretKey,
		BaseURL:    c.baseURL,
		Feed:       string(c.feed),
		HTTPClient: c.httpClient,
		RetryLimit: -1,
	})

	bars, err := sdkClient.GetBars(symbol, sdkmarketdata.GetBarsRequest{
		TimeFrame:  sdkmarketdata.OneDay,
		Adjustment: sdkmarketdata.AdjustmentSplit,
		// GetBarsRequest.End is documented as inclusive; BarRequest's
		// own To is the half-open [From, To) upper bound
		// marketdata.Manager expects throughout this codebase, so the
		// one-nanosecond subtraction converts between the two
		// conventions without changing BarRequest's own public
		// contract. Both bounds are forced to UTC explicitly — BarRequest
		// documents From/To as UTC, but a caller-supplied time.Time could
		// carry a different Location with the same instant, which would
		// still format to a different, wrong wall-clock string on the
		// wire (PR #327 review) — the previous hand-written client
		// always called .UTC() before formatting for the identical
		// reason.
		Start:     from.UTC(),
		End:       to.UTC().Add(-time.Nanosecond),
		PageLimit: c.pageLimit,
		Feed:      string(c.feed),
		Sort:      sdkmarketdata.SortAsc,
	})
	if err != nil {
		return nil, classifySDKErr(err)
	}
	return recordsFromSDKBars(bars)
}

// classifySDKErr maps an error returned by the SDK's GetBars/
// GetMultiBars to one of Client's sentinel errors.
//
//   - *sdkalpaca.APIError carries a real HTTP status code from a
//     completed request: classified by status exactly as this
//     package's previous hand-rolled classifyStatus already did.
//   - *jlexer.LexerError marks a response body the SDK could not
//     decode at all — a permanent condition (ErrBadRequest), not a
//     transient one: retrying an identical request against a
//     genuinely malformed response cannot succeed.
//   - Anything else (no HTTP response at all: connection failure,
//     timeout, DNS failure, or ctx-driven cancellation reaching the
//     SDK's own http.Client) is errNetwork, always transient.
func classifySDKErr(err error) error {
	var apiErr *sdkalpaca.APIError
	if errors.As(err, &apiErr) {
		return classifyStatus(apiErr.StatusCode, apiErr.Body)
	}
	var lexErr *jlexer.LexerError
	if errors.As(err, &lexErr) {
		return fmt.Errorf("alpaca: %w: decode response: %v", ErrBadRequest, err)
	}
	return fmt.Errorf("%w: %v", errNetwork, err)
}

// classifyStatus maps an HTTP status code (and a diagnostic body
// excerpt, already captured by *sdkalpaca.APIError — never a request
// header, so neither secret can leak into an error message via this
// path) to one of Client's sentinel errors.
func classifyStatus(status int, body string) error {
	excerpt := strings.TrimSpace(body)
	if len(excerpt) > 4*1024 {
		excerpt = excerpt[:4*1024]
	}
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
