package rest

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
)

// WithReportsDir sets the directory from which backtest JSON reports are
// served. Call before Serve. Defaults to /srv/trading/backtests/reports.
func (s *Server) WithReportsDir(dir string) {
	s.reportsDir = dir
}

func (s *Server) effectiveReportsDir() string {
	if s.reportsDir != "" {
		return s.reportsDir
	}
	return "/srv/trading/backtests/reports"
}

// ── GET /api/v1/backtests ─────────────────────────────────────────────────

// handleListBacktests scans the reports directory for *.json files and
// returns their summaries as an array, sorted in reverse filename order.
func (s *Server) handleListBacktests(w http.ResponseWriter, r *http.Request) {
	dir := s.effectiveReportsDir()
	summaries, err := service.ListBacktestSummaries(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("list backtests: %v", err))
		return
	}

	// Apply optional query filters: instrument, strategy.
	instrument := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("instrument")))
	strategy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("strategy")))

	filtered := make([]trader.BacktestReportSummary, 0, len(summaries))
	for _, summary := range summaries {
		if instrument != "" && !strings.Contains(strings.ToUpper(summary.Instrument), instrument) {
			continue
		}
		if strategy != "" && !strings.Contains(strings.ToLower(summary.Strategy), strategy) {
			continue
		}
		// Omit trade_details from list view to keep payload small.
		summary.TradeDetails = nil
		filtered = append(filtered, summary)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":     len(filtered),
		"summaries": filtered,
	})
}

// ── GET /api/v1/backtests/{name} ──────────────────────────────────────────

// handleGetBacktest returns the full summary (including trade_details) for
// a single named backtest report.
func (s *Server) handleGetBacktest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	// Sanitise: strip any path separators.
	name = filepath.Base(name)
	if !strings.HasSuffix(name, ".json") {
		name += ".json"
	}

	summary, err := service.ReadBacktestSummaryByName(s.effectiveReportsDir(), name)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, fmt.Sprintf("report %q not found", name))
		} else {
			writeErr(w, http.StatusInternalServerError, fmt.Sprintf("read report: %v", err))
		}
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// ── GET /api/v1/backtests/{name}/org ──────────────────────────────────────

// handleGetBacktestOrg serves the raw org-mode report for download.
func (s *Server) handleGetBacktestOrg(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	data, filename, err := service.ReadBacktestOrgReport(s.effectiveReportsDir(), name)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "org report not found")
		} else {
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ── GET /api/v1/backtests/{name}/candles ──────────────────────────────────

// candleBar is the JSON shape expected by lightweight-charts: unix-second
// time plus OHLC as floats.
type candleBar struct {
	Time  int64   `json:"time"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}

// handleGetBacktestCandles returns the OHLC bars for the instrument/timeframe/
// date range described in the named report's embedded config. Bars are sorted
// oldest-first. Returns an empty array (not null) when no candle files exist.
func (s *Server) handleGetBacktestCandles(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	if !strings.HasSuffix(name, ".json") {
		name += ".json"
	}
	summary, err := service.ReadBacktestSummaryByName(s.effectiveReportsDir(), name)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, fmt.Sprintf("report %q not found", name))
		} else {
			writeErr(w, http.StatusInternalServerError, fmt.Sprintf("read report: %v", err))
		}
		return
	}

	cfg := summary.Config.Data
	tr, err := trader.ParseTimeRange(cfg.From, cfg.To, cfg.Timeframe)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("parse time range: %v", err))
		return
	}

	dm := trader.NewDataManager([]string{cfg.Instrument}, tr.Start.Time(), tr.End.Time())
	iter, err := dm.Candles(r.Context(), trader.CandleRequest{
		Source:     cfg.Source, // empty → DataManager defaults to SourceOanda
		Instrument: cfg.Instrument,
		Range:      tr,
		Strict:     false,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("load candles: %v", err))
		return
	}
	defer func() { _ = iter.Close() }()

	bars := make([]candleBar, 0)
	for iter.Next() {
		c := iter.Candle()
		bars = append(bars, candleBar{
			Time:  int64(iter.Timestamp()),
			Open:  c.Open.Float64(),
			High:  c.High.Float64(),
			Low:   c.Low.Float64(),
			Close: c.Close.Float64(),
		})
	}
	if err := iter.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("read candles: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instrument": cfg.Instrument,
		"timeframe":  cfg.Timeframe,
		"bars":       bars,
	})
}
