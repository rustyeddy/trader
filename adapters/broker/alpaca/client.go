package alpaca

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rustyeddy/trader/clock"
)

// Sentinel errors Client returns (wrapped), classifying a failed request
// without requiring a caller to inspect an HTTP status code directly.
// See wireshape.go's classifyStatus/classifyOrderStatus for exactly
// which status maps to which sentinel.
var (
	// ErrUnauthorized marks an HTTP 401/403 response.
	ErrUnauthorized = errors.New("alpaca: unauthorized")
	// ErrBadRequest marks an HTTP 400/404 response from a non-order-
	// scoped endpoint.
	ErrBadRequest = errors.New("alpaca: bad request")
	// ErrOrderNotFound marks an HTTP 404 response from an order-scoped
	// endpoint (get/cancel/replace by id) — distinct from ErrBadRequest,
	// see classifyOrderStatus.
	ErrOrderNotFound = errors.New("alpaca: order not found")
	// ErrOrderRejected marks an HTTP 422 response to an order submit,
	// classified as an order-level rejection rather than a transport
	// failure. See classifyStatus's own doc comment for the confidence
	// caveat on this mapping.
	ErrOrderRejected = errors.New("alpaca: order rejected")
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
	// errNetwork marks a failure from HTTPDoer.Do itself (no HTTP
	// response at all), always treated as transient.
	errNetwork = errors.New("alpaca: network error")
)

// DefaultPaperBaseURL is Alpaca's real, published paper-trading Trading
// API base URL — distinct from https://api.alpaca.markets for live
// trading. A composition root may use this directly or supply its own.
const DefaultPaperBaseURL = "https://paper-api.alpaca.markets"

// HTTPDoer is the minimal seam Client issues requests through. Satisfied
// by *http.Client; tests inject a fake implementation, so no unit test
// in this package ever makes a real network call.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// rateLimiter and its two implementations mirror
// marketdata/internal/provider/alpaca's identical pacing mechanism,
// copied in shape (not import — see the package doc comment).
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

// ClientConfig holds Client's explicit dependencies. Configuration is a
// composition-root concern: Client never reads environment variables or
// files itself.
type ClientConfig struct {
	// BaseURL is Alpaca's Trading API base, for example
	// DefaultPaperBaseURL. Required.
	BaseURL string
	// Credential supplies the key ID/secret key pair for every request.
	// Required.
	Credential CredentialProvider

	// MaxAttempts bounds how many times a transiently-failing request is
	// attempted in total. Non-positive selects a package default (3).
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

	// clock is an internal test seam, unexported so no caller can inject
	// a non-deterministic dependency through Config.
	clock clock.Clock
}

const (
	defaultMaxAttempts    = 3
	defaultRetryBaseDelay = 500 * time.Millisecond
)

// Client is a minimal Alpaca Trading API v2 client covering account,
// positions, and order submit/get/list/cancel/replace (issue #301,
// EQ-08). It is the only network-facing type in this package. Client is
// safe for concurrent use: any method may be called from multiple
// goroutines against the same Client, and the rate limiter serializes
// and paces their requests against each other.
type Client struct {
	baseURL     string
	credential  CredentialProvider
	http        HTTPDoer
	clock       clock.Clock
	limiter     rateLimiter
	maxAttempts int
	retryBase   time.Duration
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
	return &Client{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		credential:  cfg.Credential,
		http:        httpClient,
		clock:       cl,
		limiter:     limiter,
		maxAttempts: maxAttempts,
		retryBase:   retryBase,
	}, nil
}

// do issues one HTTP request, retrying transient failures per Client's
// policy, and decodes a JSON response of type T. method/path/body
// describe the request; classify converts a non-2xx status into a
// sentinel error. A 204 No Content response (Alpaca's real cancel-order
// response) decodes to the zero T without error when expectNoContent is
// true.
func do[T any](ctx context.Context, c *Client, method, path string, body any, classify func(int, io.Reader) error, expectNoContent bool) (T, error) {
	var zero T
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if attempt > 1 {
			delay := c.retryBase * time.Duration(1<<uint(attempt-2))
			timer := c.clock.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return zero, ctx.Err()
			case <-timer.C():
			}
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return zero, err
		}

		result, err := c.doOnce(ctx, method, path, body, classify, expectNoContent)
		if err == nil {
			var v T
			if result != nil {
				if decodeErr := json.Unmarshal(result, &v); decodeErr != nil {
					return zero, fmt.Errorf("alpaca: %w: decode response: %v", ErrBadRequest, decodeErr)
				}
			}
			return v, nil
		}
		if !isTransient(err) {
			return zero, err
		}
		lastErr = err
	}
	return zero, fmt.Errorf("alpaca: %w after %d attempts: %v", ErrRetriesExhausted, c.maxAttempts, lastErr)
}

func (c *Client) doOnce(ctx context.Context, method, path string, body any, classify func(int, io.Reader) error, expectNoContent bool) ([]byte, error) {
	keyID, secretKey, err := c.credential.Credentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("alpaca: resolve credentials: %w", err)
	}

	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("alpaca: %w: %v", ErrBadRequest, err)
	}

	var reqBody *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("alpaca: %w: encode request: %v", ErrBadRequest, err)
		}
		reqBody = bytes.NewReader(encoded)
	} else {
		reqBody = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("alpaca: %w: %v", ErrBadRequest, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Never logged, never included in an error message: both secrets are
	// only ever placed on these two outgoing request headers.
	req.Header.Set("APCA-API-KEY-ID", keyID)
	req.Header.Set("APCA-API-SECRET-KEY", secretKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errNetwork, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent || (expectNoContent && resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classify(resp.StatusCode, resp.Body)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("alpaca: %w: read response: %v", ErrBadRequest, err)
	}
	return data, nil
}

// GetAccount fetches the authenticated account's current state.
//
// GET /v2/account.
func (c *Client) GetAccount(ctx context.Context) (wireAccount, error) {
	return do[wireAccount](ctx, c, http.MethodGet, "/v2/account", nil, classifyStatus, false)
}

// ListPositions fetches every open equity position.
//
// GET /v2/positions.
func (c *Client) ListPositions(ctx context.Context) ([]wirePosition, error) {
	return do[[]wirePosition](ctx, c, http.MethodGet, "/v2/positions", nil, classifyStatus, false)
}

// SubmitOrder submits a new order.
//
// POST /v2/orders. On success, Alpaca returns the created order (usually
// StatusPendingSubmit-equivalent "new" or, for a market order, sometimes
// already "filled" if it executed within the request/response window).
// On an order-level rejection (see classifyStatus), the returned error
// wraps ErrOrderRejected; the caller (Submit, in account.go) is
// responsible for converting that into a StatusRejected order.Order
// rather than propagating it as a plain error.
func (c *Client) SubmitOrder(ctx context.Context, req wireOrderSubmit) (wireOrder, error) {
	return do[wireOrder](ctx, c, http.MethodPost, "/v2/orders", req, classifyStatus, false)
}

// GetOrder fetches one order by Alpaca's own order id.
//
// GET /v2/orders/{id}. A 404 wraps ErrOrderNotFound.
func (c *Client) GetOrder(ctx context.Context, alpacaOrderID string) (wireOrder, error) {
	return do[wireOrder](ctx, c, http.MethodGet, "/v2/orders/"+url.PathEscape(alpacaOrderID), nil, classifyOrderStatus, false)
}

// GetOrderByClientOrderID resolves Trader's own OrderID (sent as
// client_order_id at submission) back to Alpaca's native order,
// including its id — used to recover the id/order-cancel correlation
// this adapter's own in-memory map would otherwise lose across an
// adapter restart.
//
// GET /v2/orders:by_client_order_id?client_order_id=... — Alpaca's real,
// documented lookup-by-client-order-id endpoint. A 404 wraps
// ErrOrderNotFound.
func (c *Client) GetOrderByClientOrderID(ctx context.Context, clientOrderID string) (wireOrder, error) {
	path := "/v2/orders:by_client_order_id?client_order_id=" + url.QueryEscape(clientOrderID)
	return do[wireOrder](ctx, c, http.MethodGet, path, nil, classifyOrderStatus, false)
}

// ListOrdersOptions constrains ListOrders.
type ListOrdersOptions struct {
	// Status selects which orders to return: "open" (Alpaca's default),
	// "closed", or "all".
	Status string
	// After, when non-zero, restricts results to orders whose
	// UpdatedAt/created_at is strictly after this instant (Alpaca's real
	// "after" query parameter), ascending order.
	After time.Time
	// Limit bounds the number of orders returned. Non-positive omits the
	// parameter (Alpaca's own default applies).
	Limit int
}

// ListOrders lists orders matching opts, oldest first.
//
// GET /v2/orders?status=...&after=...&direction=asc&limit=....
func (c *Client) ListOrders(ctx context.Context, opts ListOrdersOptions) ([]wireOrder, error) {
	q := url.Values{}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if !opts.After.IsZero() {
		q.Set("after", opts.After.UTC().Format(time.RFC3339Nano))
	}
	if opts.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	q.Set("direction", "asc")
	return do[[]wireOrder](ctx, c, http.MethodGet, "/v2/orders?"+q.Encode(), nil, classifyStatus, false)
}

// CancelOrder requests cancellation of one order by Alpaca's own order
// id.
//
// DELETE /v2/orders/{id}. A 204 means the cancel request was accepted
// (Alpaca cancels asynchronously — the order is not necessarily
// StatusCanceled yet). A 404 wraps ErrOrderNotFound. A 422 means the
// order cannot currently be canceled (already filled/canceled); the
// caller (Cancel, in account.go) re-fetches the order's actual current
// state in that case, since DELETE's own response carries no body.
func (c *Client) CancelOrder(ctx context.Context, alpacaOrderID string) error {
	_, err := do[struct{}](ctx, c, http.MethodDelete, "/v2/orders/"+url.PathEscape(alpacaOrderID), nil, classifyOrderStatus, true)
	return err
}

// ReplaceOrder requests a quantity/price amendment to an existing order.
//
// PATCH /v2/orders/{id}. Alpaca's response is the resulting order —
// whether it keeps the original id or Alpaca mints a new one on replace
// is not verified in this implementation (see wireshape.go); the caller
// (Replace, in account.go) treats whatever id this response reports as
// the order's current Alpaca identity going forward, handling either
// case correctly without needing to know which is real.
func (c *Client) ReplaceOrder(ctx context.Context, alpacaOrderID string, req wireOrderReplace) (wireOrder, error) {
	return do[wireOrder](ctx, c, http.MethodPatch, "/v2/orders/"+url.PathEscape(alpacaOrderID), req, classifyOrderStatus, false)
}
