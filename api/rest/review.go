package rest

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/rustyeddy/trader/service"
)

// GET /api/v1/review
//
// Runs a multi-timeframe watchlist review and returns triage buckets
// (Watch/Hot/Tradeable) for each requested instrument. Market data only —
// no account scoping.
func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var instruments []string
	if v := q.Get("instruments"); v != "" {
		for p := range strings.SplitSeq(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				instruments = append(instruments, p)
			}
		}
	}

	resp, err := s.svc.ReviewWatchlist(r.Context(), service.ReviewRequest{
		Instruments: instruments,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("review watchlist: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
