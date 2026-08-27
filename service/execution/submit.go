package execution

import (
	"context"
	"log/slog"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/pipeline"
)

// snapshot fetches accountID's fresh account state: OpenAccount then
// Snapshot, the same "coordinate account snapshot retrieval" step
// both Evaluate and Submit need before delegating to the injected
// *pipeline.Pipeline. It is still a point-in-time read, not an atomic
// snapshot-and-submit transaction — see the package doc comment.
func (s *Service) snapshot(ctx context.Context, accountID id.AccountID) (account.Snapshot, error) {
	acc, err := s.broker.OpenAccount(ctx, accountID)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "account opened", "open account failed", accountID, err)
		return account.Snapshot{}, err
	}

	snap, err := acc.Snapshot(ctx)
	if err != nil {
		s.logOutcome(ctx, slog.LevelDebug, "snapshot read", "snapshot read failed", accountID, err)
		return account.Snapshot{}, err
	}
	return snap, nil
}

// pipelineInput builds the pipeline.Input req and snap together
// describe — shared by Evaluate and Submit so both delegate to
// *pipeline.Pipeline with identical inputs.
func pipelineInput(req SubmitRequest, snap account.Snapshot) pipeline.Input {
	return pipeline.Input{
		Intent:          req.Intent,
		Listing:         req.Listing,
		Account:         snap,
		RiskFraction:    req.RiskFraction,
		AdverseDistance: req.AdverseDistance,
		ReferencePrice:  req.ReferencePrice,
	}
}

// toResponse converts a pipeline.Result into a SubmitResponse,
// carrying over exactly the fields pipeline.Result populated —
// Request and Order both remain zero when Evaluate/Submit did not
// reach that stage.
func toResponse(result pipeline.Result) SubmitResponse {
	return SubmitResponse{Proposal: result.Proposal, Decision: result.Decision, Request: result.Request, Order: result.Order}
}

// Evaluate implements the read-only Evaluate use case: fetch
// req.AccountID's fresh account state, then carry req.Intent through
// the read-only canonical M4 path (sizing, execution planning, risk
// admission, and — only on approval — building the approved
// order.Request) via the injected *pipeline.Pipeline.Evaluate. The
// broker is never mutated or submitted to: Evaluate's own OpenAccount
// call is the read-only Snapshot fetch every use case needs (see
// snapshot), and it never calls broker.Account.Submit.
//
// A risk rejection (errors.Is(err, pipeline.ErrRejected)) returns a
// SubmitResponse with Proposal and Decision populated and Request left
// at its zero value. A sizing, planning, or risk-evaluation error
// propagates from pipeline.Pipeline.Evaluate already wrapped but
// errors.Is-checkable against its own package's sentinel; an
// OpenAccount or Snapshot error is returned as-is, since this layer
// does no wrapping of its own.
func (s *Service) Evaluate(ctx context.Context, req SubmitRequest) (SubmitResponse, error) {
	if err := req.Validate(); err != nil {
		return SubmitResponse{}, err
	}

	snap, err := s.snapshot(ctx, req.AccountID)
	if err != nil {
		return SubmitResponse{}, err
	}

	result, err := s.pipeline.Evaluate(ctx, pipelineInput(req, snap))
	resp := toResponse(result)
	s.logEvaluateOutcome(ctx, req, resp, err)
	return resp, err
}

// Submit implements the mutating Submit use case: fetch
// req.AccountID's fresh account state, then carry req.Intent through
// the full canonical M4 path (sizing, execution planning, risk
// admission, and — only on approval — broker submission) via the
// injected *pipeline.Pipeline.Submit.
//
// A risk rejection (errors.Is(err, pipeline.ErrRejected)) returns a
// SubmitResponse with Proposal and Decision populated and Request/
// Order left at their zero values (order.Request and order.Order are
// structs, never pointers) — the broker is never called for a
// rejected proposal, matching Pipeline's own guarantee. A sizing,
// planning, or broker-submission error propagates from
// pipeline.Pipeline.Submit already wrapped but errors.Is-checkable
// against its own package's sentinel; an OpenAccount or Snapshot error
// is returned as-is, since this layer does no wrapping of its own.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) (SubmitResponse, error) {
	if err := req.Validate(); err != nil {
		return SubmitResponse{}, err
	}

	snap, err := s.snapshot(ctx, req.AccountID)
	if err != nil {
		return SubmitResponse{}, err
	}

	result, err := s.pipeline.Submit(ctx, pipelineInput(req, snap))
	resp := toResponse(result)
	s.logSubmitOutcome(ctx, req, resp, err)
	return resp, err
}
