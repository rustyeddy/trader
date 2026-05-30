package rest

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rustyeddy/trader/service"
)

// ── POST /api/v1/replay ───────────────────────────────────────────────────────
//
// Runs a strategy against stored local candles and returns the full bar series
// plus every signal emitted by the strategy. Signals include opens, closes,
// regime-blocked events, no-stop drops, and trailing stop updates.
//
// Request body: service.ReplayRequest JSON
// Response:     service.ReplayResult JSON
//
// Example:
//
//	POST /api/v1/replay
//	{
//	  "instrument": "EURUSD",
//	  "timeframe":  "H1",
//	  "from":       "2026-01-01",
//	  "to":         "2026-05-29",
//	  "warmup_bars": 200,
//	  "strategy":   {"kind": "donchianv6"},
//	  "exit":       {"kind": "chandelier", "params": {"period": 14, "multiplier": 3.0}},
//	  "regime":     {"kind": ""}
//	}

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	var req service.ReplayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	if req.Instrument == "" {
		writeErr(w, http.StatusBadRequest, "instrument is required")
		return
	}
	if req.From == "" || req.To == "" {
		writeErr(w, http.StatusBadRequest, "from and to dates are required")
		return
	}
	if req.Timeframe == "" {
		req.Timeframe = "H1"
	}

	result, err := s.svc.RunReplay(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("replay: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, result)
}
