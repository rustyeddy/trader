package rest

import (
	"fmt"
	"net/http"

	trader "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
)

// POST /api/v1/analysis
//
// Accepts a multipart form upload with field name "file".
// Returns the parsed rows split into three slices: tradeable, watchlist, no_trade.
func (s *Server) handleAnalysis(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("parse form: %v", err))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, `multipart field "file" is required`)
		return
	}
	defer file.Close()

	rows, err := service.ParseAnalysisCSV(file)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("parse csv: %v", err))
		return
	}

	result := partitionAnalysis(rows)
	writeJSON(w, http.StatusOK, result)
}

type analysisResult struct {
	Total     int                    `json:"total"`
	Tradeable []trader.ForexAnalysis `json:"tradeable"`
	Watchlist []trader.ForexAnalysis `json:"watchlist"`
	NoTrade   []trader.ForexAnalysis `json:"no_trade"`
}

func partitionAnalysis(rows []trader.ForexAnalysis) analysisResult {
	result := analysisResult{
		Total:     len(rows),
		Tradeable: []trader.ForexAnalysis{},
		Watchlist: []trader.ForexAnalysis{},
		NoTrade:   []trader.ForexAnalysis{},
	}
	for _, r := range rows {
		switch r.Status {
		case trader.StatusTradeable:
			result.Tradeable = append(result.Tradeable, r)
		case trader.StatusWatchlist:
			result.Watchlist = append(result.Watchlist, r)
		default:
			result.NoTrade = append(result.NoTrade, r)
		}
	}
	return result
}
