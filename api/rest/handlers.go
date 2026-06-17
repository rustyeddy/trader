package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
		for p := range strings.SplitSeq(q, ",") {
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

// ── GET /api/v1/candles/{instrument}/stats ────────────────────────────────
// Query: timeframe (default H1), from (YYYY-MM-DD), to (YYYY-MM-DD), units (int)

func (s *Server) handleCandleStats(w http.ResponseWriter, r *http.Request) {
	instrument := r.PathValue("instrument")
	if instrument == "" {
		writeErr(w, http.StatusBadRequest, "instrument required")
		return
	}
	q := r.URL.Query()
	from := q.Get("from")
	to := q.Get("to")
	if from == "" || to == "" {
		writeErr(w, http.StatusBadRequest, "from and to are required (YYYY-MM-DD)")
		return
	}
	tf := q.Get("timeframe")
	if tf == "" {
		tf = "H1"
	}
	var units int64
	if u := q.Get("units"); u != "" {
		parsed, err := strconv.ParseInt(u, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "units must be an integer")
			return
		}
		units = parsed
	}
	result, err := s.svc.CandleStats(r.Context(), service.CandleStatsRequest{
		Instrument: instrument,
		Timeframe:  tf,
		From:       from,
		To:         to,
		Source:     q.Get("source"),
		Units:      units,
	})
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("candle stats: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── GET /api/v1/candles/validate ─────────────────────────────────────────
// Query: instruments (CSV), from (YYYY-MM), to (YYYY-MM), timeframe, source

func (s *Server) handleValidateCandles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	instrumentsCSV := q.Get("instruments")
	if instrumentsCSV == "" {
		writeErr(w, http.StatusBadRequest, "instruments required (comma-separated)")
		return
	}
	fromStr := q.Get("from")
	toStr := q.Get("to")
	if fromStr == "" || toStr == "" {
		writeErr(w, http.StatusBadRequest, "from and to required (YYYY-MM)")
		return
	}

	var instruments []string
	for p := range strings.SplitSeq(instrumentsCSV, ",") {
		if p = strings.TrimSpace(p); p != "" {
			instruments = append(instruments, p)
		}
	}
	from, err := time.Parse("2006-01", fromStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("bad from: %v", err))
		return
	}
	// to is inclusive; end of that month exclusive = first day of next month.
	toMonth, err := time.Parse("2006-01", toStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("bad to: %v", err))
		return
	}
	toExcl := toMonth.AddDate(0, 1, 0)

	tf := q.Get("timeframe")
	if tf == "" {
		tf = "H1"
	}
	src := q.Get("source")
	if src == "" {
		src = "oanda"
	}

	report, err := s.svc.ValidateCandleData(r.Context(), service.ValidateCandleDataRequest{
		Instruments: instruments,
		Source:      src,
		Timeframe:   tf,
		From:        from,
		To:          toExcl,
	})
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("validate candles: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// ── GET /api/v1/pip-values ────────────────────────────────────────────────
// Query: units (int, default 100000), instruments (CSV, default all majors)

func (s *Server) handlePipValues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var units int64 = 100_000
	if u := q.Get("units"); u != "" {
		parsed, err := strconv.ParseInt(u, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "units must be an integer")
			return
		}
		units = parsed
	}
	var instruments []string
	if iv := q.Get("instruments"); iv != "" {
		for p := range strings.SplitSeq(iv, ",") {
			if p = strings.TrimSpace(p); p != "" {
				instruments = append(instruments, p)
			}
		}
	}
	result, err := s.svc.PipValues(r.Context(), service.PipValuesRequest{
		Units:       units,
		Instruments: instruments,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("pip values: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── GET /api/v1/position ──────────────────────────────────────────────────
// Query: instrument (required), price (float), units (int), notional (float), pips (float)

func (s *Server) handlePosition(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	instrument := q.Get("instrument")
	if instrument == "" {
		writeErr(w, http.StatusBadRequest, "instrument required")
		return
	}

	parseFloat := func(key string) float64 {
		if v := q.Get(key); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
		return 0
	}
	parseInt := func(key string) int64 {
		if v := q.Get(key); v != "" {
			if i, err := strconv.ParseInt(v, 10, 64); err == nil {
				return i
			}
		}
		return 0
	}

	result, err := s.svc.PositionCalc(r.Context(), service.PositionCalcRequest{
		Instrument: instrument,
		Price:      parseFloat("price"),
		Units:      parseInt("units"),
		Notional:   parseFloat("notional"),
		Pips:       parseFloat("pips"),
	})
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("position calc: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, result)
}
