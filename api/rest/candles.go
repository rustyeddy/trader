package rest

import (
	"fmt"
	"net/http"

	datasvc "github.com/rustyeddy/trader/service/data"
)

// ── GET /api/v1/candles/{instrument} ───────────────────────────────────────

func (s *Server) handleGetCandlesCSV(w http.ResponseWriter, r *http.Request) {
	instrument := r.PathValue("instrument")
	result, err := (&datasvc.Service{OANDA: s.oanda}).CandlesCSV(r.Context(), datasvc.CandlesCSVRequest{
		Instrument: instrument,
		Timeframe:  r.URL.Query().Get("timeframe"),
		From:       r.URL.Query().Get("from"),
		To:         r.URL.Query().Get("to"),
		Source:     r.URL.Query().Get("source"),
	})
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("candles: %v", err))
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("X-Candle-Count", fmt.Sprintf("%d", result.Count))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(result.CSV))
}
