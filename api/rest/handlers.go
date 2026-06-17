package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rustyeddy/trader/service"
)

// ── GET /api/v1/account ───────────────────────────────────────────────────

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requireOANDA(w) {
		return
	}
	summary, err := s.svc.GetAccountSummary(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("get account: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// ── GET /api/v1/trades ────────────────────────────────────────────────────

func (s *Server) handleListTrades(w http.ResponseWriter, r *http.Request) {
	if !s.requireOANDA(w) {
		return
	}
	trades, err := s.svc.ListOpenTrades(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("list trades: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, trades)
}

// ── POST /api/v1/trades ───────────────────────────────────────────────────

type placeOrderRequest struct {
	Instrument string  `json:"instrument"`
	Side       string  `json:"side"`
	RiskPct    float64 `json:"risk_pct"`
	StopPips   float64 `json:"stop_pips"`
	StopPrice  float64 `json:"stop_price"`
	Units      int64   `json:"units"`
	Confirm    bool    `json:"confirm"`
}

func (s *Server) handlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	if !s.requireOANDA(w) {
		return
	}
	var req placeOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	result, err := s.svc.PlaceMarketOrder(r.Context(), service.PlaceMarketOrderRequest{
		Instrument: req.Instrument,
		Side:       req.Side,
		RiskPct:    req.RiskPct,
		StopPips:   req.StopPips,
		StopPrice:  req.StopPrice,
		Units:      req.Units,
		Confirm:    req.Confirm,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("place order: %v", err))
		return
	}
	status := http.StatusOK
	if req.Confirm && result.Filled != nil {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

// ── PATCH /api/v1/trades/{id}/stop ────────────────────────────────────────

type updateStopRequest struct {
	StopPrice float64 `json:"stop_price"`
	TakePrice float64 `json:"take_price"`
}

func (s *Server) handleUpdateStop(w http.ResponseWriter, r *http.Request) {
	if !s.requireOANDA(w) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "trade id required")
		return
	}
	var req updateStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	if err := s.svc.UpdateTradeStop(r.Context(), id, req.StopPrice, req.TakePrice); err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("update stop: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"trade_id": id, "status": "updated"})
}

// ── DELETE /api/v1/trades/{id} ────────────────────────────────────────────

func (s *Server) handleCloseTrade(w http.ResponseWriter, r *http.Request) {
	if !s.requireOANDA(w) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "trade id required")
		return
	}
	var units int64
	if u := r.URL.Query().Get("units"); u != "" {
		parsed, err := strconv.ParseInt(u, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "units must be an integer")
			return
		}
		units = parsed
	}
	result, err := s.svc.CloseTrade(r.Context(), id, units)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("close trade: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── GET /api/v1/transactions ──────────────────────────────────────────────

func (s *Server) handleGetTransactions(w http.ResponseWriter, r *http.Request) {
	if !s.requireOANDA(w) {
		return
	}
	var sinceID int64
	if s := r.URL.Query().Get("since_id"); s != "" {
		parsed, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "since_id must be an integer")
			return
		}
		sinceID = parsed
	}
	txns, lastID, err := s.svc.GetTransactions(r.Context(), sinceID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("get transactions: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"transactions":        txns,
		"last_transaction_id": lastID,
	})
}

// ── GET /api/v1/prices ───────────────────────────────────────────────────

func (s *Server) handleGetPrices(w http.ResponseWriter, r *http.Request) {
	if !s.requireOANDA(w) {
		return
	}
	var instruments []string
	if q := r.URL.Query().Get("instruments"); q != "" {
		for _, p := range strings.Split(q, ",") {
			if p = strings.TrimSpace(p); p != "" {
				instruments = append(instruments, p)
			}
		}
	}
	prices, err := s.svc.GetPrices(r.Context(), service.GetPricesRequest{
		Instruments: instruments,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("get prices: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prices": prices})
}

// ── POST /api/v1/backtests/run ────────────────────────────────────────────

type runBacktestRequest struct {
	// ConfigPaths is a list of YAML backtest config file paths on the server.
	ConfigPaths []string `json:"config_paths"`
}

func (s *Server) handleRunBacktest(w http.ResponseWriter, r *http.Request) {
	var req runBacktestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	if len(req.ConfigPaths) == 0 {
		writeErr(w, http.StatusBadRequest, "config_paths is required")
		return
	}

	summaries, err := s.svc.RunBacktestPathSpecsAndWriteReports(r.Context(), req.ConfigPaths, s.effectiveReportsDir())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("run backtest: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":     len(summaries),
		"summaries": summaries,
	})
}
