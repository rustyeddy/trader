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

	"github.com/rustyeddy/trader/internal/accountactivity"
)

// Errors classifying a failed request, so callers need not read status codes.
// Nothing is retried here: the sync loop is replay-safe, so a failed run is
// simply run again.
var (
	ErrUnauthorized        = errors.New("oanda: unauthorized")
	ErrBadRequest          = errors.New("oanda: bad request")
	ErrRateLimited         = errors.New("oanda: rate limited")
	ErrProviderUnavailable = errors.New("oanda: provider unavailable")
	ErrUnexpectedStatus    = errors.New("oanda: unexpected status")
	ErrBadResponse         = errors.New("oanda: malformed response")
)

// CredentialProvider supplies the bearer token, called before every request
// so a rotating credential works. It must never expose the token.
type CredentialProvider interface {
	Token(ctx context.Context) (string, error)
}

// HTTPDoer is the seam all requests go through; *http.Client satisfies it.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Base URLs of OANDA's two v20 REST environments.
const (
	PracticeURL = "https://api-fxpractice.oanda.com"
	LiveURL     = "https://api-fxtrade.oanda.com"
)

// BaseURL returns the REST base URL for env.
func BaseURL(env accountactivity.Environment) (string, error) {
	switch env {
	case accountactivity.EnvironmentPractice:
		return PracticeURL, nil
	case accountactivity.EnvironmentLive:
		return LiveURL, nil
	default:
		return "", fmt.Errorf("oanda: environment %q has no base URL", env)
	}
}

// maxBody bounds a response body read; an idrange page is far smaller.
const maxBody = 64 << 20

// Client is a minimal read-only OANDA v20 client for account transactions.
type Client struct {
	base  string
	http  HTTPDoer
	creds CredentialProvider
}

// NewClient returns a Client. base is a scheme+host without a trailing path
// (PracticeURL, LiveURL, or a test server's URL).
func NewClient(base string, doer HTTPDoer, creds CredentialProvider) (*Client, error) {
	if base == "" || doer == nil || creds == nil {
		return nil, errors.New("oanda: base URL, HTTP client and credential provider are required")
	}
	return &Client{base: strings.TrimRight(base, "/"), http: doer, creds: creds}, nil
}

// get issues a GET and decodes the JSON body into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	token, err := c.creds.Token(ctx)
	if err != nil {
		return fmt.Errorf("oanda: credential: %w", err)
	}
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("oanda: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Datetime-Format", "RFC3339")

	resp, err := c.http.Do(req)
	if err != nil {
		// Report the path and the underlying cause, never the request.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("oanda: GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("oanda: reading %s: %w", path, err)
	}
	if resp.StatusCode/100 != 2 {
		return statusError(path, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrBadResponse, path, err)
	}
	return nil
}

func statusError(path string, code int) error {
	var sentinel error
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		sentinel = ErrUnauthorized
	case code == http.StatusBadRequest || code == http.StatusNotFound:
		sentinel = ErrBadRequest
	case code == http.StatusTooManyRequests:
		sentinel = ErrRateLimited
	case code >= 500:
		sentinel = ErrProviderUnavailable
	default:
		sentinel = ErrUnexpectedStatus
	}
	return fmt.Errorf("%w: GET %s: status %d", sentinel, path, code)
}

// Summary is the part of the account summary the importer needs.
type Summary struct {
	// Currency is the account's home currency code.
	Currency string
	// LastTransactionID is the newest transaction ID on the account.
	LastTransactionID int64
}

type summaryWire struct {
	Account struct {
		Currency          string `json:"currency"`
		LastTransactionID string `json:"lastTransactionID"`
	} `json:"account"`
	LastTransactionID string `json:"lastTransactionID"`
}

// AccountSummary fetches the account's home currency and newest transaction ID.
func (c *Client) AccountSummary(ctx context.Context, account string) (Summary, error) {
	var w summaryWire
	if err := c.get(ctx, "/v3/accounts/"+url.PathEscape(account)+"/summary", nil, &w); err != nil {
		return Summary{}, err
	}
	last := w.LastTransactionID
	if last == "" {
		last = w.Account.LastTransactionID
	}
	id, err := strconv.ParseInt(last, 10, 64)
	if err != nil || id < 0 {
		return Summary{}, fmt.Errorf("%w: summary lastTransactionID %q", ErrBadResponse, last)
	}
	if w.Account.Currency == "" {
		return Summary{}, fmt.Errorf("%w: summary has no currency", ErrBadResponse)
	}
	return Summary{Currency: w.Account.Currency, LastTransactionID: id}, nil
}

// IDWindow is an inclusive transaction ID range.
type IDWindow struct{ First, Last int64 }

// Empty reports whether the window holds no IDs.
func (w IDWindow) Empty() bool { return w.Last < w.First }

type listWire struct {
	Count int      `json:"count"`
	Pages []string `json:"pages"`
}

// ResolveWindow maps one time window to the transaction ID window it covers,
// using the transactions list endpoint, whose pages are idrange links. OANDA
// treats both from and to as inclusive and limits a request to 365 days, so
// callers split longer spans and pass an inclusive end (Source does). An empty
// window is returned (not an error) when no transactions fall inside.
func (c *Client) ResolveWindow(ctx context.Context, account string, from, to time.Time) (IDWindow, error) {
	q := url.Values{}
	if !from.IsZero() {
		q.Set("from", from.UTC().Format(time.RFC3339Nano))
	}
	if !to.IsZero() {
		q.Set("to", to.UTC().Format(time.RFC3339Nano))
	}
	q.Set("pageSize", "1000")
	var w listWire
	if err := c.get(ctx, "/v3/accounts/"+url.PathEscape(account)+"/transactions", q, &w); err != nil {
		return IDWindow{}, err
	}
	win := IDWindow{First: 1<<62 - 1, Last: -1}
	for _, page := range w.Pages {
		u, err := url.Parse(page)
		if err != nil {
			return IDWindow{}, fmt.Errorf("%w: page link %q", ErrBadResponse, page)
		}
		first, err1 := strconv.ParseInt(u.Query().Get("from"), 10, 64)
		last, err2 := strconv.ParseInt(u.Query().Get("to"), 10, 64)
		if err1 != nil || err2 != nil || first < 0 || last < first {
			return IDWindow{}, fmt.Errorf("%w: page link %q", ErrBadResponse, page)
		}
		win.First = min(win.First, first)
		win.Last = max(win.Last, last)
	}
	if win.Last < 0 {
		if w.Count > 0 {
			// The response says transactions matched but links none of them.
			// Treating that as an empty window would end the import
			// successfully without the history.
			return IDWindow{}, fmt.Errorf("%w: list reports %d transactions but no pages", ErrBadResponse, w.Count)
		}
		return IDWindow{First: 1, Last: 0}, nil // no pages: empty
	}
	return win, nil
}

// idrangeWire's Transactions is a pointer so a missing or null array (nil)
// is told apart from an explicit empty one. A response that omits the array
// must not look like a page with nothing in it.
type idrangeWire struct {
	Transactions *[]json.RawMessage `json:"transactions"`
}

// IDRange fetches the transactions with IDs in [from, to], inclusive, as raw
// JSON objects in the order OANDA returned them.
func (c *Client) IDRange(ctx context.Context, account string, from, to int64) ([]json.RawMessage, error) {
	q := url.Values{}
	q.Set("from", strconv.FormatInt(from, 10))
	q.Set("to", strconv.FormatInt(to, 10))
	var w idrangeWire
	if err := c.get(ctx, "/v3/accounts/"+url.PathEscape(account)+"/transactions/idrange", q, &w); err != nil {
		return nil, err
	}
	if w.Transactions == nil {
		return nil, fmt.Errorf("%w: idrange response has no transactions array", ErrBadResponse)
	}
	return *w.Transactions, nil
}
