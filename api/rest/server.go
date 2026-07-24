// Package rest is the HTTP presentation layer over the service package.
// No business logic lives here; each handler calls a service method and
// maps the result to JSON or an HTTP error.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/config"
	accountsvc "github.com/rustyeddy/trader/service/account"
)

// Server exposes account/backtest/review/etc. functionality over HTTP. It
// holds no business logic itself — each handler calls into the relevant
// service/<domain> package and maps the result to JSON or an HTTP error.
type Server struct {
	oanda      *oanda.Client // nil for backtest-only use; OANDA-requiring endpoints respond 503
	log        *slog.Logger
	accountID  string
	backtests  backtest.BacktestExecutor
	addr       string
	staticFS   fs.FS  // nil when no UI assets are embedded
	reportsDir string // directory for backtest JSON reports
	configsDir string // directory for backtest config files

	reviewSweepReportsDir string       // directory for review-sweep JSON reports
	reviewSweepConfigsDir string       // directory for review-sweep config files
	mcpHandler            http.Handler // optional MCP handler mounted at POST /mcp
}

// New creates a Server. oandaClient may be nil for backtest-only use;
// endpoints that require OANDA will respond 503 in that case. log may be
// nil, in which case slog.Default() is used.
func New(oandaClient *oanda.Client, log *slog.Logger, accountID string, backtests backtest.BacktestExecutor, addr string) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{oanda: oandaClient, log: log, accountID: accountID, backtests: backtests, addr: addr}
}

// WithMCPHandler mounts an MCP HTTP handler at POST /mcp so that the MCP
// server is reachable on the same port as the REST API when running
// trader serve. Pass the result of mcp.Server.HTTPHandler().
func (s *Server) WithMCPHandler(h http.Handler) {
	s.mcpHandler = h
}

// WithStatic sets the fs.FS from which the UI static assets are served.
// Call before Serve. The FS should be rooted at the dist/ directory
// (i.e. "index.html" should open directly, not "dist/index.html").
func (s *Server) WithStatic(fsys fs.FS) {
	s.staticFS = fsys
}

// Handler returns the http.Handler (mux) with all routes wired up.
// Exposed so tests can call ServeHTTP directly without binding a port.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Accounts — list the accounts the token can see and the default one.
	// summary/orders mirror `trader account summary`/`trader account orders`:
	// un-scoped reads with an optional ?account_id= query param.
	mux.HandleFunc("GET /api/v1/accounts", s.handleListAccounts)
	mux.HandleFunc("GET /api/v1/accounts/default", s.handleDefaultAccount)
	mux.HandleFunc("GET /api/v1/accounts/summary", s.handleAccountSummary)
	mux.HandleFunc("GET /api/v1/accounts/orders", s.handleAccountOrders)

	// Account & trades (OANDA required). All account-specific operations are
	// scoped to an explicit account: /api/v1/accounts/{accountID}/…. There are
	// no un-scoped account routes — mutations must always name their account.
	const acct = "/api/v1/accounts/{accountID}"
	mux.HandleFunc("GET "+acct+"/account", s.handleGetAccount)
	mux.HandleFunc("GET "+acct+"/trades", s.handleListTrades)
	mux.HandleFunc("POST "+acct+"/trades", s.handlePlaceOrder)
	mux.HandleFunc("PATCH "+acct+"/trades/{id}/stop", s.handleUpdateStop)
	mux.HandleFunc("DELETE "+acct+"/trades/{id}", s.handleCloseTrade)
	mux.HandleFunc("GET "+acct+"/transactions", s.handleGetTransactions)
	mux.HandleFunc("POST "+acct+"/bots", s.handleStartBot)
	mux.HandleFunc("GET "+acct+"/bots", s.handleListBots)
	mux.HandleFunc("GET "+acct+"/stream/account", s.handleStreamAccount)
	mux.HandleFunc("GET "+acct+"/stream/events", s.handleStreamEvents)

	// Prices are market data, not account-specific; this read defaults to the
	// first account internally and stays un-scoped.
	mux.HandleFunc("GET /api/v1/prices", s.handleGetPrices)

	mux.HandleFunc("GET /api/v1/candles/validate", s.handleValidateCandles)
	mux.HandleFunc("GET /api/v1/candles/{instrument}", s.handleGetCandlesCSV)
	mux.HandleFunc("GET /api/v1/candles/{instrument}/stats", s.handleDataStats)
	mux.HandleFunc("GET /api/v1/pip-values", s.handlePipValues)
	mux.HandleFunc("GET /api/v1/position", s.handlePosition)

	// Backtests — run + browse saved reports
	mux.HandleFunc("POST /api/v1/backtests/run", s.handleRunBacktest)
	mux.HandleFunc("POST /api/v1/backtests/regress", s.handleRegressBacktest)
	mux.HandleFunc("GET /api/v1/backtests/configs", s.handleListBacktestConfigs)
	mux.HandleFunc("GET /api/v1/backtests", s.handleListBacktests)
	mux.HandleFunc("GET /api/v1/backtests/{name}", s.handleGetBacktest)
	mux.HandleFunc("GET /api/v1/backtests/{name}/org", s.handleGetBacktestOrg)
	mux.HandleFunc("GET /api/v1/backtests/{name}/candles", s.handleGetBacktestCandles)

	// Strategy replay — runs a strategy against stored candles, returns bars + signals
	mux.HandleFunc("POST /api/v1/replay", s.handleReplay)

	// Watchlist review — multi-timeframe triage buckets (Watch/Hot/Tradeable)
	mux.HandleFunc("GET /api/v1/review", s.handleReview)

	// Review sweep — historical classification sweeps, run from persisted
	// YAML configs and stored as named JSON reports (mirrors the backtest
	// config/report mechanism above).
	mux.HandleFunc("GET /api/v1/review-sweeps/configs", s.handleListReviewSweepConfigs)
	mux.HandleFunc("POST /api/v1/review-sweeps/run", s.handleRunReviewSweep)
	mux.HandleFunc("GET /api/v1/review-sweeps", s.handleListReviewSweeps)
	mux.HandleFunc("GET /api/v1/review-sweeps/{name}", s.handleGetReviewSweep)

	// Bot manager — get/stop by globally-unique bot ID (start/list are
	// account-scoped above).
	mux.HandleFunc("GET /api/v1/bots/{id}", s.handleGetBot)
	mux.HandleFunc("DELETE /api/v1/bots/{id}", s.handleStopBot)

	// SSE streams (account/events are account-scoped above).
	mux.HandleFunc("GET /api/v1/stream/backtest/{id}", s.handleStreamBacktest)

	// Health check — both paths for orchestrators and API clients.
	health := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
	mux.HandleFunc("GET /api/v1/health", health)
	mux.HandleFunc("GET /health", health)

	// Version
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": config.Version})
	})

	// MCP over HTTP — optional; enabled when trader serve starts with MCP.
	if s.mcpHandler != nil {
		mux.Handle("POST /mcp", s.mcpHandler)
	}

	// Static UI — registered last so /api/* routes take priority.
	if s.staticFS != nil {
		mux.Handle("/", s.spaHandler())
	}

	return corsMiddleware(mux)
}

// spaHandler serves static files from s.staticFS with an SPA fallback:
// unknown paths get index.html so client-side routing works.
func (s *Server) spaHandler() http.Handler {
	fileServer := http.FileServerFS(s.staticFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		// Check if the file exists in the embedded FS.
		f, err := s.staticFS.Open(path)
		if err != nil {
			// SPA fallback — let the client-side router handle it.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}

// Serve starts listening on s.addr and blocks until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	srv := &http.Server{
		Addr:         s.addr,
		Handler:      s.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.log.Info("rest: listening", "addr", ln.Addr().String())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// corsMiddleware adds permissive CORS headers so a browser-based front-end
// can call the API during development. Tighten in production.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeJSON marshals v as indented JSON and writes it with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// writeErr writes a {"error": msg} JSON body with the given HTTP status.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// requireOANDA returns false and writes 503 if the OANDA client is absent.
func (s *Server) requireOANDA(w http.ResponseWriter) bool {
	if s.oanda == nil {
		writeErr(w, http.StatusServiceUnavailable,
			"OANDA integration not configured (no token); backtest-only mode")
		return false
	}
	return true
}

// resolveAccount returns the account a request targets. Scoped routes carry an
// {accountID} path value and resolve to exactly that account. The few un-scoped
// read routes (e.g. prices) carry none and resolve to the first/default account
// — appropriate only for reads, never for mutations. Writes the appropriate
// HTTP error and returns ok=false when OANDA is unconfigured or the account
// cannot be resolved.
func (s *Server) resolveAccount(w http.ResponseWriter, r *http.Request) (*account.Account, bool) {
	if !s.requireOANDA(w) {
		return nil, false
	}
	var (
		acc *account.Account
		err error
	)
	if id := r.PathValue("accountID"); id != "" {
		acc, err = accountsvc.Resolve(r.Context(), id, s.oanda, s.log)
	} else {
		acc, err = accountsvc.ResolveFirst(r.Context(), s.accountID, s.oanda, s.log)
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("resolve account: %v", err))
		return nil, false
	}
	return acc, true
}
