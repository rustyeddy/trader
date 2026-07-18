package service

import (
	"context"

	replaysvc "github.com/rustyeddy/trader/service/replay"
)

// RunReplay runs a strategy against stored candles and returns every bar
// plus every signal the strategy emitted. See
// service/replay.Service.RunReplay.
func (s *Service) RunReplay(ctx context.Context, req ReplayRequest) (*ReplayResult, error) {
	return (&replaysvc.Service{}).RunReplay(ctx, req)
}

// Types re-exported as aliases so existing call sites
// (service.ReplayRequest{...} etc.) keep compiling unchanged while the
// implementation lives in service/replay.
type (
	SignalKind    = replaysvc.SignalKind
	Signal        = replaysvc.Signal
	ReplayResult  = replaysvc.ReplayResult
	ReplayRequest = replaysvc.ReplayRequest
)
