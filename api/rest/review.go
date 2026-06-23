package rest

import (
	"fmt"
	"net/http"

	"github.com/rustyeddy/trader/review"
	"github.com/rustyeddy/trader/service"
)

// POST /api/v1/review
//
// Accepts a multipart form upload with field name "file".
// Returns the parsed rows split into three slices: tradeable, watchlist, no_trade.
func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
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

	rows, err := service.ParseReviewCSV(file)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("parse csv: %v", err))
		return
	}

	result := partitionReview(rows)
	writeJSON(w, http.StatusOK, result)
}

type reviewResult struct {
	Total     int                  `json:"total"`
	Tradeable []review.ForexReview `json:"tradeable"`
	Watchlist []review.ForexReview `json:"watchlist"`
	NoTrade   []review.ForexReview `json:"no_trade"`
}

func partitionReview(rows []review.ForexReview) reviewResult {
	result := reviewResult{
		Total:     len(rows),
		Tradeable: []review.ForexReview{},
		Watchlist: []review.ForexReview{},
		NoTrade:   []review.ForexReview{},
	}
	for _, r := range rows {
		switch r.Status {
		case review.StatusTradeable:
			result.Tradeable = append(result.Tradeable, r)
		case review.StatusWatchlist:
			result.Watchlist = append(result.Watchlist, r)
		default:
			result.NoTrade = append(result.NoTrade, r)
		}
	}
	return result
}
