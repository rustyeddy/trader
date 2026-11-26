package rest

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/service"
)

// BarsPath is the one endpoint.
const BarsPath = "/v1/marketdata/bars"

// DefaultTimeout bounds one request when Config names none.
const DefaultTimeout = 30 * time.Second

// ReadBarsFunc is what the handler needs from the application service:
// service.ReadBars, or a fake in a test.
type ReadBarsFunc func(ctx context.Context, req service.BarsRequest) (service.BarsPage, error)

// Config is what NewHandler needs. The transport owner (cmd/trader) fills it from its
// flags and environment; nothing here reads either.
type Config struct {
	ReadBars ReadBarsFunc
	// Token, when not empty, is the bearer token every request must present. Empty
	// means no authentication: only for a server that listens on loopback.
	Token string
	// Timeout bounds one request; zero is DefaultTimeout.
	Timeout time.Duration
	// Log receives one line per failed request; nil is silent. It never receives a
	// token or a header.
	Log *slog.Logger
}

// NewHandler returns the API's http.Handler.
func NewHandler(cfg Config) http.Handler {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	h := &handler{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc(BarsPath, h.bars)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint; the API has "+BarsPath, nil)
	})
	return h.authenticated(mux)
}

type handler struct{ cfg Config }

// authenticated requires the bearer token when one is configured. The comparison is
// constant-time and the token never appears in a response or a log.
func (h *handler) authenticated(next http.Handler) http.Handler {
	if h.cfg.Token == "" {
		return next
	}
	want := []byte(h.cfg.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="trader"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// params are the query parameters the endpoint knows. Anything else is refused, so a
// misspelled parameter is an error and not silently ignored.
var params = map[string]bool{
	"source": true, "instrument": true, "interval": true, "from": true, "to": true,
	"as_of": true, "limit": true, "cursor": true, "partial": true,
}

func (h *handler) bars(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET", nil)
		return
	}
	req, query, perr := parseBarsRequest(r)
	if perr != nil {
		writeError(w, http.StatusBadRequest, perr.code, perr.msg, nil)
		return
	}
	if query.cursor != "" {
		after, err := decodeCursor(query.cursor, query.fingerprint)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", err.Error(), nil)
			return
		}
		req.After = after
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.cfg.Timeout)
	defer cancel()
	page, err := h.cfg.ReadBars(ctx, req)
	switch {
	case err == nil:
	case errors.Is(err, service.ErrInvalidBarsRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	case errors.Is(err, service.ErrNoData):
		cov := coverageOf(page.Coverage)
		writeError(w, http.StatusNotFound, "no_data", err.Error(), &cov)
		return
	case errors.Is(err, service.ErrBarsIncomplete):
		cov := coverageOf(page.Coverage)
		writeError(w, http.StatusUnprocessableEntity, "incomplete", err.Error(), &cov)
		return
	case errors.Is(err, service.ErrBarsMixed):
		writeError(w, http.StatusUnprocessableEntity, "mixed_series", err.Error(), nil)
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "timeout", "the request took too long", nil)
		return
	case errors.Is(err, context.Canceled):
		writeError(w, 499, "cancelled", "the request was cancelled", nil)
		return
	default:
		h.cfg.Log.Error("bars request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "the request could not be completed", nil)
		return
	}

	out := barsJSON{Source: page.Source, Instrument: page.Instrument, Interval: page.Interval,
		From: page.From.UTC(), To: page.To.UTC(), Bars: make([]barJSON, 0, len(page.Bars)), Coverage: coverageOf(page.Coverage), Data: dataOf(page.Data)}
	if !page.AsOf.IsZero() {
		asOf := page.AsOf.UTC()
		out.AsOf = &asOf
	}
	for _, b := range page.Bars {
		out.Bars = append(out.Bars, barOf(b))
	}
	if !page.NextAfter.IsZero() {
		out.NextCursor = encodeCursor(page.NextAfter, query.fingerprint)
	}
	writeJSON(w, http.StatusOK, out)
}

type paramError struct{ code, msg string }

type query struct {
	cursor string
	// fingerprint identifies the query apart from the page size and the position, so a
	// cursor only continues the query it came from.
	fingerprint string
}

// parseBarsRequest normalizes the query string into a service request: the only
// work the transport does with the parameters. Names are upper-cased (instrument)
// or lower-cased (interval, source), times are RFC 3339 or a UTC date, and every
// failure is a client error naming the parameter.
func parseBarsRequest(r *http.Request) (service.BarsRequest, query, *paramError) {
	var req service.BarsRequest
	var q query
	vals := r.URL.Query()
	for k, v := range vals {
		if !params[k] {
			return req, q, &paramError{"invalid_parameter", fmt.Sprintf("unknown parameter %q", k)}
		}
		if len(v) != 1 {
			return req, q, &paramError{"invalid_parameter", fmt.Sprintf("parameter %q given %d times", k, len(v))}
		}
	}
	get := func(k string) string { return strings.TrimSpace(vals.Get(k)) }

	req.Source = strings.ToLower(get("source"))
	req.Instrument = strings.ToUpper(get("instrument"))
	req.Interval = strings.ToLower(get("interval"))
	if req.Instrument == "" {
		return req, q, &paramError{"invalid_parameter", "instrument is required"}
	}
	if req.Interval == "" {
		return req, q, &paramError{"invalid_parameter", "interval is required (h1 or d1)"}
	}
	var err error
	if req.From, err = parseTime("from", get("from"), true); err != nil {
		return req, q, &paramError{"invalid_parameter", err.Error()}
	}
	if req.To, err = parseTime("to", get("to"), true); err != nil {
		return req, q, &paramError{"invalid_parameter", err.Error()}
	}
	if s := get("as_of"); s != "" {
		if req.AsOf, err = parseTime("as_of", s, false); err != nil {
			return req, q, &paramError{"invalid_parameter", err.Error()}
		}
	}
	if s := get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > service.MaxBarsLimit {
			return req, q, &paramError{"invalid_parameter", fmt.Sprintf("limit must be a whole number from 1 to %d", service.MaxBarsLimit)}
		}
		req.Limit = n
	}
	partial := true // partial is the default: serve what is there and say what is not
	if s := get("partial"); s != "" {
		if partial, err = strconv.ParseBool(s); err != nil {
			return req, q, &paramError{"invalid_parameter", `partial must be "true" or "false"`}
		}
	}
	req.Strict = !partial
	q.cursor = get("cursor")
	q.fingerprint = fingerprint(req)
	return req, q, nil
}

// parseTime reads an RFC 3339 instant (any offset, converted to UTC) or, when date
// is true, a UTC calendar date.
func parseTime(name, s string, required bool) (time.Time, error) {
	if s == "" {
		if required {
			return time.Time{}, fmt.Errorf("%s is required (RFC 3339, for example 2024-01-01T00:00:00Z)", name)
		}
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%s %q is not an RFC 3339 time or a YYYY-MM-DD date", name, s)
}

// fingerprint identifies the query a cursor belongs to.
func fingerprint(req service.BarsRequest) string {
	key := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%t", req.Source, req.Instrument, req.Interval,
		req.From.UTC().Format(time.RFC3339Nano), req.To.UTC().Format(time.RFC3339Nano), req.AsOf.UTC().Format(time.RFC3339Nano), req.Strict)
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:16]
}

type cursorJSON struct {
	V int    `json:"v"`
	Q string `json:"q"`
	A string `json:"a"`
}

// encodeCursor makes the opaque token that continues a query after the bar opening at
// after. Clients treat it as a string; its shape is not part of the contract.
func encodeCursor(after time.Time, fingerprint string) string {
	b, _ := json.Marshal(cursorJSON{V: 1, Q: fingerprint, A: after.UTC().Format(time.RFC3339Nano)})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(token, fingerprint string) (time.Time, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, errors.New("cursor is not a cursor this API issued")
	}
	var c cursorJSON
	if err := json.Unmarshal(raw, &c); err != nil || c.V != 1 {
		return time.Time{}, errors.New("cursor is not a cursor this API issued")
	}
	after, err := time.Parse(time.RFC3339Nano, c.A)
	if err != nil {
		return time.Time{}, errors.New("cursor is not a cursor this API issued")
	}
	if c.Q != fingerprint {
		return time.Time{}, errors.New("cursor belongs to a different query: keep instrument, interval, from, to, as_of and partial the same")
	}
	return after, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // a write error means the client has gone; there is nobody to tell
}

func writeError(w http.ResponseWriter, status int, code, msg string, cov *coverageJSON) {
	writeJSON(w, status, errorBody{Error: errorJSON{Code: code, Message: msg}, Coverage: cov})
}
