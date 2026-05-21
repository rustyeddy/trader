package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
)

// ── SSE writer ────────────────────────────────────────────────────────────

// sseWriter wraps a ResponseWriter with Server-Sent Events helpers.
// The caller must check the ok return from newSSEWriter before using it.
type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // prevent nginx from buffering
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &sseWriter{w: w, f: f}, true
}

// send writes a named event with a JSON-encoded data field.
func (s *sseWriter) send(event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if event != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", event); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

// comment writes an SSE comment line — useful as a keep-alive ping.
func (s *sseWriter) comment(text string) {
	fmt.Fprintf(s.w, ": %s\n\n", text)
	s.f.Flush()
}

// ── GET /api/v1/stream/account ────────────────────────────────────────────

// handleStreamAccount polls the account summary every 5 s and pushes each
// snapshot as an "account" SSE event. The connection stays open until the
// client disconnects.
func (s *Server) handleStreamAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requireOANDA(w) {
		return
	}
	sse, ok := newSSEWriter(w)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported by this server")
		return
	}

	ctx := r.Context()

	// Send an initial snapshot immediately.
	if summary, err := s.svc.GetAccountSummary(ctx); err == nil {
		_ = sse.send("account", summary)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			summary, err := s.svc.GetAccountSummary(ctx)
			if err != nil {
				sse.comment("error: " + err.Error())
				continue
			}
			if err := sse.send("account", summary); err != nil {
				return // client disconnected
			}
		}
	}
}

// ── GET /api/v1/stream/events ─────────────────────────────────────────────

// handleStreamEvents subscribes to the OANDA transaction stream and forwards
// each transaction as a "transaction" SSE event. Heartbeats are forwarded as
// "heartbeat" events to keep proxies from closing idle connections.
func (s *Server) handleStreamEvents(w http.ResponseWriter, r *http.Request) {
	if !s.requireOANDA(w) {
		return
	}
	sse, ok := newSSEWriter(w)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported by this server")
		return
	}

	ctx := r.Context()

	ch, err := s.svc.StreamTransactions(ctx, oanda.StreamOptions{
		OnHeartbeat: func(hb oanda.Heartbeat) {
			_ = sse.send("heartbeat", map[string]any{
				"time":   hb.Time,
				"lastID": hb.LastTxID,
			})
		},
	})
	if err != nil {
		sse.comment("error: " + err.Error())
		return
	}

	for ev := range ch {
		if ev.Err != nil {
			sse.comment("stream error: " + ev.Err.Error())
			continue
		}
		if err := sse.send("transaction", ev.Tx); err != nil {
			return
		}
	}
}

// ── GET /api/v1/stream/backtest/{id} ─────────────────────────────────────

// handleStreamBacktest is a placeholder for in-flight backtest progress
// (#113+). Returns a 501 until the backtest runner supports progress hooks.
func (s *Server) handleStreamBacktest(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented,
		"backtest progress streaming not yet implemented (see issue #113)")
}
