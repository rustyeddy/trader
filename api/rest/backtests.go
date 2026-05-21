package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rustyeddy/trader"
)

// WithReportsDir sets the directory from which backtest JSON reports are
// served. Call before Serve. Defaults to "../trading/backtests" relative
// to the working directory when empty.
func (s *Server) WithReportsDir(dir string) {
	s.reportsDir = dir
}

func (s *Server) effectiveReportsDir() string {
	if s.reportsDir != "" {
		return s.reportsDir
	}
	return "../trading/backtests"
}

// ── GET /api/v1/backtests ─────────────────────────────────────────────────

// handleListBacktests scans the reports directory for *.json files and
// returns their summaries as an array, sorted newest-first by name.
func (s *Server) handleListBacktests(w http.ResponseWriter, r *http.Request) {
	dir := s.effectiveReportsDir()
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("glob: %v", err))
		return
	}

	// Sort newest-first by filename (names include timestamps or are stable).
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))

	// Apply optional query filters: instrument, strategy.
	instrument := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("instrument")))
	strategy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("strategy")))

	var summaries []trader.BacktestReportSummary
	for _, path := range matches {
		s, err := loadSummary(path)
		if err != nil {
			continue
		}
		if instrument != "" && !strings.Contains(strings.ToUpper(s.Instrument), instrument) {
			continue
		}
		if strategy != "" && !strings.Contains(strings.ToLower(s.Strategy), strategy) {
			continue
		}
		// Omit trade_details from list view to keep payload small.
		s.TradeDetails = nil
		summaries = append(summaries, s)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":     len(summaries),
		"summaries": summaries,
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

	path := filepath.Join(s.effectiveReportsDir(), name)
	summary, err := loadSummary(path)
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
	if !strings.HasSuffix(name, ".org") {
		name += ".org"
	}
	path := filepath.Join(s.effectiveReportsDir(), name)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "org report not found")
		} else {
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// loadSummary reads and parses a BacktestReportSummary from a JSON file.
func loadSummary(path string) (trader.BacktestReportSummary, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return trader.BacktestReportSummary{}, err
	}
	var s trader.BacktestReportSummary
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	// Backfill name from filename when the JSON field is empty.
	if s.Name == "" {
		base := filepath.Base(path)
		s.Name = strings.TrimSuffix(base, ".json")
	}
	return s, nil
}
