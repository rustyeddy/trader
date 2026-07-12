package rest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/service"
)

// isWithinDir reports whether path is equal to or nested inside base after
// both are cleaned. It returns false on any error.
// Note: symlinks are not resolved; a symlink that points outside base may
// pass this check. Ensure mount/link policies are enforced at the OS level.
func isWithinDir(base, path string) bool {
	base = filepath.Clean(base)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

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

	filtered := make([]backtest.BacktestReportSummary, 0, len(summaries))
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

// ── GET /api/v1/backtests/configs ─────────────────────────────────────────

// handleListBacktestConfigs scans the configs directory for *.yml, *.yaml,
// and *.json files and returns their names sorted lexically.
// The directory defaults to $TRADER_BACKTEST_DIR/configs or
// /srv/trading/backtests/configs and may be overridden with ?dir=.
func (s *Server) handleListBacktestConfigs(w http.ResponseWriter, r *http.Request) {
	dir := strings.TrimSpace(r.URL.Query().Get("dir"))
	if dir == "" {
		dir = s.effectiveConfigsDir()
	} else {
		dir = filepath.Clean(dir)
		// Restrict ?dir= overrides to within the configured configs directory.
		// If no configs directory is configured, the override is not allowed to
		// prevent clients enumerating arbitrary server paths.
		if s.configsDir == "" || !isWithinDir(s.configsDir, dir) {
			writeErr(w, http.StatusBadRequest, "dir must be within the configured configs directory")
			return
		}
	}

	var matches []string
	for _, pat := range []string{"*.yml", "*.yaml", "*.json"} {
		m, err := filepath.Glob(filepath.Join(dir, pat))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, fmt.Sprintf("glob %s: %v", pat, err))
			return
		}
		matches = append(matches, m...)
	}
	sort.Strings(matches)

	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":   len(names),
		"configs": names,
		"dir":     dir,
	})
}

func (s *Server) effectiveConfigsDir() string {
	if s.configsDir != "" {
		return s.configsDir
	}
	return "/srv/trading/backtests/configs"
}

// ── POST /api/v1/backtests/regress ────────────────────────────────────────

type regressBacktestRequest struct {
	// ConfigPaths is a list of YAML backtest config file paths on the server.
	// Defaults to the configs directory when omitted.
	ConfigPaths []string `json:"config_paths,omitempty"`
	// Update, when true, writes the current results as new baselines instead
	// of comparing against existing ones.
	Update bool `json:"update,omitempty"`
	// BaselineDir is the directory holding committed baseline JSON reports.
	// Defaults to the reports directory when omitted.
	BaselineDir string `json:"baseline_dir,omitempty"`
}

type regressResult struct {
	Name   string   `json:"name"`
	Passed bool     `json:"passed"`
	Diffs  []string `json:"diffs,omitempty"`
}

// handleRegressBacktest runs backtest configs and compares results against
// committed JSON baselines, returning per-run pass/fail details.
func (s *Server) handleRegressBacktest(w http.ResponseWriter, r *http.Request) {
	if s.svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "service not configured")
		return
	}
	var req regressBacktestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}

	configPaths := req.ConfigPaths
	if len(configPaths) == 0 {
		configPaths = []string{s.effectiveConfigsDir()}
	}

	baselineDir := strings.TrimSpace(req.BaselineDir)
	if baselineDir == "" {
		baselineDir = s.effectiveReportsDir()
	} else {
		baselineDir = filepath.Clean(baselineDir)
		if !isWithinDir(s.effectiveReportsDir(), baselineDir) {
			writeErr(w, http.StatusBadRequest, "baseline_dir must be within the configured reports directory")
			return
		}
	}

	summaries, err := s.svc.RunBacktestPathSpecs(r.Context(), configPaths)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("run backtests: %v", err))
		return
	}
	if len(summaries) == 0 {
		writeErr(w, http.StatusUnprocessableEntity, "no backtest results generated from provided config paths")
		return
	}

	if req.Update {
		if err := os.MkdirAll(baselineDir, 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, fmt.Sprintf("create baseline dir: %v", err))
			return
		}
		for _, s2 := range summaries {
			safeName := filepath.Base(s2.Name)
			path := filepath.Join(baselineDir, safeName+".json")
			if err := service.WriteBacktestSummaryJSON(path, s2); err != nil {
				writeErr(w, http.StatusInternalServerError, fmt.Sprintf("write baseline for %q: %v", s2.Name, err))
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"updated":      len(summaries),
			"baseline_dir": baselineDir,
		})
		return
	}

	var results []regressResult
	anyFailed := false
	for _, got := range summaries {
		safeName := filepath.Base(got.Name)
		path := filepath.Join(baselineDir, safeName+".json")
		baseline, err := service.ReadBacktestSummaryFile(path)
		if err != nil {
			results = append(results, regressResult{
				Name:   got.Name,
				Passed: false,
				Diffs:  []string{fmt.Sprintf("no baseline at %s", path)},
			})
			anyFailed = true
			continue
		}
		diffs := diffBacktestSummaries(baseline, got)
		passed := len(diffs) == 0
		if !passed {
			anyFailed = true
		}
		results = append(results, regressResult{Name: got.Name, Passed: passed, Diffs: diffs})
	}

	status := http.StatusOK
	if anyFailed {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, map[string]any{
		"passed":  !anyFailed,
		"results": results,
	})
}

// diffBacktestSummaries returns human-readable diff strings for changed metrics.
func diffBacktestSummaries(baseline, got backtest.BacktestReportSummary) []string {
	var diffs []string
	diffInt := func(field string, b, g int) {
		if b != g {
			diffs = append(diffs, fmt.Sprintf("%s: baseline=%d got=%d", field, b, g))
		}
	}
	diffFloat := func(field string, b, g float64) {
		if b != g {
			diffs = append(diffs, fmt.Sprintf("%s: baseline=%v got=%v", field, b, g))
		}
	}
	diffInt("trades", baseline.Trades, got.Trades)
	diffInt("wins", baseline.Wins, got.Wins)
	diffInt("losses", baseline.Losses, got.Losses)
	diffInt("spread_filtered", baseline.SpreadFiltered, got.SpreadFiltered)
	diffFloat("start_balance", baseline.StartBalance, got.StartBalance)
	diffFloat("end_balance", baseline.EndBalance, got.EndBalance)
	diffFloat("net_pl", baseline.NetPL, got.NetPL)
	diffFloat("return_pct", baseline.ReturnPct, got.ReturnPct)
	diffFloat("win_rate", baseline.WinRate, got.WinRate)
	diffFloat("max_drawdown", baseline.MaxDrawdown, got.MaxDrawdown)
	diffFloat("avg_winner", baseline.AvgWinner, got.AvgWinner)
	diffFloat("avg_loser", baseline.AvgLoser, got.AvgLoser)
	diffFloat("rr", baseline.RR, got.RR)
	diffFloat("avg_spread_pips", baseline.AvgSpreadPips, got.AvgSpreadPips)
	return diffs
}

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
	tr, err := market.ParseTimeRange(cfg.From, cfg.To, cfg.Timeframe)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("parse time range: %v", err))
		return
	}

	dm := datamanager.NewDataManager([]string{cfg.Instrument}, tr.Start.Time(), tr.End.Time())
	iter, err := dm.Candles(r.Context(), datamanager.CandleRequest{
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
	for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
		c := ct.Candle
		bars = append(bars, candleBar{
			Time:  int64(ct.Timestamp),
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
