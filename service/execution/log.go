package execution

import (
	"context"
	"errors"
	"log/slog"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/pipeline"
)

// logOutcome emits exactly one record for a plain operational step
// (OpenAccount, Snapshot): levelOK when err is nil, ERROR when it is
// not. Mirrors service/broker's own "log the operation boundary once"
// convention.
func (s *Service) logOutcome(ctx context.Context, levelOK slog.Level, okMsg, failMsg string, accountID id.AccountID, err error, attrs ...any) {
	base := attrs
	if !accountID.IsZero() {
		base = append([]any{logging.AccountID, accountID.String()}, base...)
	}
	if err != nil {
		s.logger.ErrorContext(ctx, failMsg, append(base, "error", err)...)
		return
	}
	s.logger.Log(ctx, levelOK, okMsg, base...)
}

// logSubmitOutcome logs Submit's own pipeline outcome. A risk
// rejection (errors.Is(err, pipeline.ErrRejected)) is logged as an
// expected, structured admission decision at Info level — allowed and
// the request-identifying attributes — never at error severity: a
// risk rejection is a normal business outcome of evaluating risk, not
// an operational service failure (#186 review). Every other pipeline
// error (sizing, planning, or broker submission) still logs at error
// severity, matching every other failure this package logs.
func (s *Service) logSubmitOutcome(ctx context.Context, req SubmitRequest, result SubmitResponse, err error) {
	attrs := []any{
		logging.AccountID, req.AccountID.String(),
		logging.InstrumentID, req.Listing.InstrumentID().String(),
		"intent_kind", req.Intent.Kind.String(),
	}

	switch {
	case err == nil:
		attrs = append(attrs, "allowed", true,
			logging.OrderID, result.Order.Request.OrderID.String(),
			"status", result.Order.Status.String())
		s.logger.InfoContext(ctx, "execution submitted", attrs...)
	case errors.Is(err, pipeline.ErrRejected):
		attrs = append(attrs, "allowed", false)
		s.logger.InfoContext(ctx, "execution rejected", attrs...)
	default:
		attrs = append(attrs, "error", err)
		s.logger.ErrorContext(ctx, "execution failed", attrs...)
	}
}
