// Package rest is the HTTP presentation layer over the service package.
// No business logic lives here; each handler calls a service method and
// maps the result to JSON or an HTTP error.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rustyeddy/trader/service"
)

// Server wraps a Service and exposes its methods over HTTP.
type Server struct {
	svc        *service.Service
	addr       string
	log        *slog.Logger
	staticFS   fs.FS         // nil when no UI assets are embedded
	reportsDir string        // directory for backtest JSON reports
	mcpHandler http.Handler  // optional MCP handler mounted at POST /mcp
}

// New creates a Server. svc may have a nil OANDA client for backtest-only
// use; endpoints that require OANDA will respond 503 in that case.
func New(svc *service.Service, addr string) *Server {
	log := svc.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{svc: svc, addr: addr, log: log}
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

	// Account & trades (OANDA required)
	mux.HandleFunc("GET /api/v1/account", s.handleGetAccount)
	mux.HandleFunc("GET /api/v1/prices", s.handleGetPrices)
	mux.HandleFunc("GET /api/v1/trades", s.handleListTrades)
	mux.HandleFunc("POST /api/v1/trades", s.handlePlaceOrder)
	mux.HandleFunc("PATCH /api/v1/trades/{id}/stop", s.handleUpdateStop)
	mux.HandleFunc("DELETE /api/v1/trades/{id}", s.handleCloseTrade)
	mux.HandleFunc("GET /api/v1/transactions", s.handleGetTransactions)
	mux.HandleFunc("GET /api/v1/candles/validate", s.handleValidateCandles)
	mux.HandleFunc("GET /api/v1/candles/{instrument}", s.handleGetCandlesCSV)
	mux.HandleFunc("GET /api/v1/candles/{instrument}/stats", s.handleCandleStats)
	mux.HandleFunc("GET /api/v1/pip-values", s.handlePipValues)
	mux.HandleFunc("GET /api/v1/position", s.handlePosition)

	// Backtests — run + browse saved reports
	mux.HandleFunc("POST /api/v1/backtests/run", s.handleRunBacktest)
	mux.HandleFunc("GET /api/v1/backtests", s.handleListBacktests)
	mux.HandleFunc("GET /api/v1/backtests/{name}", s.handleGetBacktest)
	mux.HandleFunc("GET /api/v1/backtests/{name}/org", s.handleGetBacktestOrg)
	mux.HandleFunc("GET /api/v1/backtests/{name}/candles", s.handleGetBacktestCandles)

	// Strategy replay — runs a strategy against stored candles, returns bars + signals
	mux.HandleFunc("POST /api/v1/replay", s.handleReplay)

	// Analysis — parse a ChatGPT forex analysis CSV upload
	mux.HandleFunc("POST /api/v1/analysis", s.handleAnalysis)

	// Bot manager — start/stop/list live strategy bots
	mux.HandleFunc("POST /api/v1/bots", s.handleStartBot)
	mux.HandleFunc("GET /api/v1/bots", s.handleListBots)
	mux.HandleFunc("GET /api/v1/bots/{id}", s.handleGetBot)
	mux.HandleFunc("DELETE /api/v1/bots/{id}", s.handleStopBot)

	// SSE streams
	mux.HandleFunc("GET /api/v1/stream/account", s.handleStreamAccount)
	mux.HandleFunc("GET /api/v1/stream/events", s.handleStreamEvents)
	mux.HandleFunc("GET /api/v1/stream/backtest/{id}", s.handleStreamBacktest)

	// Health check — both paths for orchestrators and API clients.
	health := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
	mux.HandleFunc("GET /api/v1/health", health)
	mux.HandleFunc("GET /health", health)

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
	if s.svc.OANDA == nil {
		writeErr(w, http.StatusServiceUnavailable,
			"OANDA integration not configured (no token); backtest-only mode")
		return false
	}
	return true
}
