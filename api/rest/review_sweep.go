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

	"github.com/rustyeddy/trader/reviewsweep"
	reviewsweepsvc "github.com/rustyeddy/trader/service/reviewsweep"
)

// reviewSweepSvc constructs a fresh reviewsweepsvc.Service from the
// server's underlying service.Service dependencies on every call — not
// cached, matching the no-caching rationale documented on
// backtestSvc/reviewSvc in the service package (a bare &service.Service{}
// struct literal must be picked up too).
func (s *Server) reviewSweepSvc() *reviewsweepsvc.Service {
	return &reviewsweepsvc.Service{Log: s.svc.Log}
}

// WithReviewSweepReportsDir sets the directory from which review-sweep
// JSON reports are served and to which new runs are persisted. Call
// before Serve. Defaults to /srv/trading/review-sweeps/reports.
func (s *Server) WithReviewSweepReportsDir(dir string) {
	s.reviewSweepReportsDir = dir
}

// WithReviewSweepConfigsDir sets the directory scanned for review-sweep
// YAML/JSON config files. Call before Serve. Defaults to
// /srv/trading/review-sweeps/configs.
func (s *Server) WithReviewSweepConfigsDir(dir string) {
	s.reviewSweepConfigsDir = dir
}

func (s *Server) effectiveReviewSweepReportsDir() string {
	if s.reviewSweepReportsDir != "" {
		return s.reviewSweepReportsDir
	}
	return "/srv/trading/review-sweeps/reports"
}

func (s *Server) effectiveReviewSweepConfigsDir() string {
	if s.reviewSweepConfigsDir != "" {
		return s.reviewSweepConfigsDir
	}
	return "/srv/trading/review-sweeps/configs"
}

// ── GET /api/v1/review-sweeps/configs ───────────────────────────────────────

// handleListReviewSweepConfigs scans the configs directory for *.yml,
// *.yaml, and *.json files and returns their names sorted lexically.
func (s *Server) handleListReviewSweepConfigs(w http.ResponseWriter, r *http.Request) {
	dir := strings.TrimSpace(r.URL.Query().Get("dir"))
	if dir == "" {
		dir = s.effectiveReviewSweepConfigsDir()
	} else {
		dir = filepath.Clean(dir)
		if s.reviewSweepConfigsDir == "" || !isWithinDir(s.reviewSweepConfigsDir, dir) {
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

// ── POST /api/v1/review-sweeps/run ──────────────────────────────────────────

type runReviewSweepRequest struct {
	// ConfigPaths is a list of YAML/JSON review-sweep config file paths on
	// the server. Defaults to the configs directory when omitted.
	ConfigPaths []string `json:"config_paths,omitempty"`
}

// handleRunReviewSweep runs review-sweep configs and persists each
// resulting summary as a named JSON report, then returns the summaries.
func (s *Server) handleRunReviewSweep(w http.ResponseWriter, r *http.Request) {
	if s.svc == nil {
		writeErr(w, http.StatusServiceUnavailable, "service not configured")
		return
	}
	var req runReviewSweepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}

	configPaths := req.ConfigPaths
	if len(configPaths) == 0 {
		configPaths = []string{s.effectiveReviewSweepConfigsDir()}
	}

	summaries, err := s.reviewSweepSvc().RunPathSpecsAndWriteReports(r.Context(), configPaths, s.effectiveReviewSweepReportsDir())
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("run review sweeps: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":     len(summaries),
		"summaries": summaries,
	})
}

// ── GET /api/v1/review-sweeps ────────────────────────────────────────────────

// handleListReviewSweeps scans the reports directory for *.json files and
// returns their summaries, sorted in reverse filename order. Results are
// omitted from the list view to keep payload small — fetch
// GET /api/v1/review-sweeps/{name} for the full per-step data.
func (s *Server) handleListReviewSweeps(w http.ResponseWriter, r *http.Request) {
	dir := s.effectiveReviewSweepReportsDir()
	summaries, err := reviewsweepsvc.ListReports(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("list review sweeps: %v", err))
		return
	}

	instrument := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("instrument")))

	filtered := make([]reviewsweep.ReportSummary, 0, len(summaries))
	for _, summary := range summaries {
		if instrument != "" && !containsInstrument(summary.Instruments, instrument) {
			continue
		}
		summary.Results = nil
		filtered = append(filtered, summary)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":     len(filtered),
		"summaries": filtered,
	})
}

// ── GET /api/v1/review-sweeps/{name} ────────────────────────────────────────

// handleGetReviewSweep returns the full report (including results) for a
// single named review-sweep report.
func (s *Server) handleGetReviewSweep(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}

	summary, err := reviewsweepsvc.ReadReportByName(s.effectiveReviewSweepReportsDir(), name)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, fmt.Sprintf("review sweep %q not found", name))
		} else {
			writeErr(w, http.StatusInternalServerError, fmt.Sprintf("read report: %v", err))
		}
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func containsInstrument(instruments []string, want string) bool {
	for _, i := range instruments {
		if strings.EqualFold(i, want) {
			return true
		}
	}
	return false
}
