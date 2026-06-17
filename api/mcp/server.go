// Package mcp implements an MCP (Model Context Protocol) server over the
// service layer. Transport is stdio (Claude Code / Claude Desktop compatible)
// or HTTP+SSE. The protocol is JSON-RPC 2.0 with newline framing.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/rustyeddy/trader/service"
)

const protocolVersion = "2024-11-05"

// Server is the MCP server. It holds the service reference, the tool and
// resource registries, and the write-enable flag for dangerous tools.
type Server struct {
	svc         *service.Service
	log         *slog.Logger
	writeEnable bool // gates place_order, close_trade, update_stop
	reportsDir  string

	mu  sync.Mutex
	out io.Writer
}

const defaultReportsDir = "/srv/trading/backtests/reports"

// New creates a Server.
func New(svc *service.Service, writeEnable bool) *Server {
	log := svc.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		svc:         svc,
		log:         log,
		writeEnable: writeEnable,
		reportsDir:  defaultReportsDir,
	}
}

// WithReportsDir overrides the directory used for persisted backtest reports
// and MCP backtest resources.
func (s *Server) WithReportsDir(dir string) {
	if dir != "" {
		s.reportsDir = dir
	}
}

func (s *Server) effectiveReportsDir() string {
	if s.reportsDir != "" {
		return s.reportsDir
	}
	return defaultReportsDir
}

// ServeStdio runs the MCP server over os.Stdin / os.Stdout until ctx is
// cancelled or EOF. This is the standard Claude Code / Claude Desktop mode.
func (s *Server) ServeStdio(ctx context.Context) error {
	s.out = os.Stdout
	return s.serve(ctx, os.Stdin)
}

// Serve reads JSON-RPC requests from r and writes responses to w.
// Used by ServeStdio and by tests.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	s.out = w
	return s.serve(ctx, r)
}

func (s *Server) serve(ctx context.Context, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
	}()
	defer close(done)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		s.handleLine(ctx, line)
	}
	return scanner.Err()
}

// ── JSON-RPC 2.0 types ───────────────────────────────────────────────────

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	errParse          = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
)

// process handles one JSON-RPC line and returns the response bytes (with
// trailing newline), or nil for notifications that require no reply.
func (s *Server) process(ctx context.Context, line []byte) []byte {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		b, _ := json.Marshal(response{JSONRPC: "2.0", Error: &rpcError{Code: errParse, Message: "parse error"}})
		return append(b, '\n')
	}
	if req.JSONRPC != "2.0" {
		b, _ := json.Marshal(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: errInvalidRequest, Message: "jsonrpc must be '2.0'"}})
		return append(b, '\n')
	}
	// Notifications have no id — dispatch but don't reply.
	if req.ID == nil || string(req.ID) == "null" {
		s.dispatchNotification(ctx, req)
		return nil
	}
	result, rpcErr := s.dispatch(ctx, req)
	var resp response
	if rpcErr != nil {
		resp = response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
	} else {
		resp = response{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	b, _ := json.Marshal(resp)
	return append(b, '\n')
}

func (s *Server) handleLine(ctx context.Context, line []byte) {
	b := s.process(ctx, line)
	if b == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.out.Write(b)
}

// HTTPHandler returns an http.Handler that accepts a single JSON-RPC POST
// body and writes the JSON-RPC response. Use this to expose MCP alongside
// the REST API when running trader serve.
//
//	POST /mcp   Content-Type: application/json
//
// Note: POST /mcp is covered by the REST server's CORS middleware, which
// allows all origins. If --mcp-enable-write is set, any browser origin can
// invoke write tools (place_order, close_trade, update_stop). Restrict the
// CORS origin or add bearer-token authentication before enabling writes in
// a production environment.
func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if ct != "" && !strings.HasPrefix(ct, "application/json") {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		resp := s.process(r.Context(), bytes.TrimSpace(body))
		// A nil response means the request was a notification; no reply is
		// required by the JSON-RPC spec.
		if resp == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(bytes.TrimRight(resp, "\n"))
	})
}

func (s *Server) dispatch(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.Params)
	case "tools/list":
		return s.handleToolsList()
	case "tools/call":
		return s.handleToolsCall(ctx, req.Params)
	case "resources/list":
		return s.handleResourcesList()
	case "resources/read":
		return s.handleResourcesRead(ctx, req.Params)
	case "prompts/list":
		return map[string]any{"prompts": []any{}}, nil
	default:
		return nil, &rpcError{Code: errMethodNotFound, Message: fmt.Sprintf("method not found: %s", req.Method)}
	}
}

func (s *Server) dispatchNotification(_ context.Context, req request) {
	switch req.Method {
	case "notifications/initialized":
		s.log.Debug("mcp: client initialized")
	default:
		s.log.Debug("mcp: unhandled notification", "method", req.Method)
	}
}

// ── initialize ────────────────────────────────────────────────────────────

type initParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

func (s *Server) handleInitialize(raw json.RawMessage) (any, *rpcError) {
	var p initParams
	if raw != nil {
		_ = json.Unmarshal(raw, &p)
	}
	s.log.Info("mcp: initialized",
		"client", p.ClientInfo.Name,
		"client_version", p.ClientInfo.Version,
		"protocol", p.ProtocolVersion,
	)
	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "trader",
			"version": "1.0",
		},
	}, nil
}


// ── MCP content helpers ───────────────────────────────────────────────────

func textContent(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": false,
	}
}

func errContent(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	}
}

func jsonContent(v any) map[string]any {
	b, _ := json.MarshalIndent(v, "", "  ")
	return textContent(string(b))
}
