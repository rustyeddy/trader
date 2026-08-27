package execution

import (
	"context"
	"log/slog"

	"github.com/rustyeddy/trader/pipeline"
)

// Submit implements the Submit use case: fetch req.AccountID's fresh
// account state, then carry req.Intent through the full canonical M4
// path (sizing, execution planning, risk admission, and — only on
// approval — broker submission) via the injected *pipeline.Pipeline.
//
// A risk rejection (errors.Is(err, pipeline.ErrRejected)) returns a
// SubmitResponse with Proposal and Decision populated and Order left
// at its zero value (order.Order is a struct, never a pointer) — the
// broker is never called for a rejected proposal, matching Pipeline's
// own guarantee. A sizing, planning, or broker-submission error
// propagates from pipeline.Pipeline.Submit already wrapped but
// errors.Is-checkable against its own package's sentinel; an
// OpenAccount or Snapshot error is returned as-is, since this layer
// does no wrapping of its own.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) (SubmitResponse, error) {
	if err := req.Validate(); err != nil {
		return SubmitResponse{}, err
	}

	acc, err := s.broker.OpenAccount(ctx, req.AccountID)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "account opened", "open account failed", req.AccountID, err)
		return SubmitResponse{}, err
	}

	snap, err := acc.Snapshot(ctx)
	if err != nil {
		s.logOutcome(ctx, slog.LevelDebug, "snapshot read", "snapshot read failed", req.AccountID, err)
		return SubmitResponse{}, err
	}

	result, err := s.pipeline.Submit(ctx, pipeline.Input{
		Intent:          req.Intent,
		Listing:         req.Listing,
		Account:         snap,
		RiskFraction:    req.RiskFraction,
		AdverseDistance: req.AdverseDistance,
		ReferencePrice:  req.ReferencePrice,
	})
	resp := SubmitResponse{Proposal: result.Proposal, Decision: result.Decision, Order: result.Order}
	s.logSubmitOutcome(ctx, req, resp, err)
	if err != nil {
		return resp, err
	}
	return resp, nil
}
