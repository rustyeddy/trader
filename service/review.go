package service

import (
	"context"

	reviewsvc "github.com/rustyeddy/trader/service/review"
)

// reviewSvc constructs a fresh reviewsvc.Service from the current field
// values on every call — see backtestSvc's doc comment in
// service/backtest.go for why this isn't cached/embedded.
func (s *Service) reviewSvc() *reviewsvc.Service {
	return &reviewsvc.Service{OANDA: s.OANDA, Log: s.Log}
}

// ReviewWatchlist runs a watchlist review scan. See
// service/review.Service.ReviewWatchlist.
func (s *Service) ReviewWatchlist(ctx context.Context, req ReviewRequest) (*ReviewResponse, error) {
	return s.reviewSvc().ReviewWatchlist(ctx, req)
}

// ReviewWatchlistRange runs a historical classification sweep. See
// service/review.Service.ReviewWatchlistRange.
func (s *Service) ReviewWatchlistRange(ctx context.Context, req ReviewRangeRequest) (*ReviewSweepResponse, error) {
	return s.reviewSvc().ReviewWatchlistRange(ctx, req)
}

// Types re-exported as aliases so existing call sites
// (service.ReviewRequest{...} etc.) keep compiling unchanged while the
// implementation lives in service/review.
type (
	ReviewRequest       = reviewsvc.ReviewRequest
	ReviewResponse      = reviewsvc.ReviewResponse
	ReviewRangeRequest  = reviewsvc.ReviewRangeRequest
	ReviewSweepResponse = reviewsvc.ReviewSweepResponse
)
