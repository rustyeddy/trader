package broker

import (
	"context"
	"log/slog"

	"github.com/rustyeddy/trader/logging"
)

// Submit implements the mutating Submit use case: ask req.AccountID's
// broker to accept req.Order as a new order.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) (SubmitResponse, error) {
	if err := req.Validate(); err != nil {
		return SubmitResponse{}, err
	}
	instrumentAttr := []any{logging.InstrumentID, req.Order.Listing.InstrumentID().String()}

	acc, err := s.broker.OpenAccount(ctx, req.AccountID)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "order submitted", "open account failed", req.AccountID, err, instrumentAttr...)
		return SubmitResponse{}, err
	}

	o, err := acc.Submit(ctx, req.Order)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "order submitted", "order submit failed", req.AccountID, err, instrumentAttr...)
		return SubmitResponse{}, err
	}
	s.logOutcome(ctx, slog.LevelInfo, "order submitted", "order submit failed", req.AccountID, nil,
		append(instrumentAttr, logging.OrderID, o.Request.OrderID.String(), "status", o.Status.String())...)
	return SubmitResponse{Order: o}, nil
}

// Cancel implements the mutating Cancel use case: ask req.AccountID's
// broker to cancel an existing order.
func (s *Service) Cancel(ctx context.Context, req CancelRequest) (CancelResponse, error) {
	if err := req.Validate(); err != nil {
		return CancelResponse{}, err
	}
	orderAttr := []any{logging.OrderID, req.Cancel.OrderID.String()}

	acc, err := s.broker.OpenAccount(ctx, req.AccountID)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "cancel requested", "open account failed", req.AccountID, err, orderAttr...)
		return CancelResponse{}, err
	}

	result, err := acc.Cancel(ctx, req.Cancel)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "cancel requested", "cancel failed", req.AccountID, err, orderAttr...)
		return CancelResponse{}, err
	}
	s.logOutcome(ctx, slog.LevelInfo, "cancel requested", "cancel failed", req.AccountID, nil,
		logging.OrderID, result.OrderID.String(), "status", result.Status.String())
	return CancelResponse{Result: result}, nil
}

// Replace implements the mutating Replace use case: ask req.AccountID's
// broker to modify an existing order's quantity and/or prices in
// place.
func (s *Service) Replace(ctx context.Context, req ReplaceRequest) (ReplaceResponse, error) {
	if err := req.Validate(); err != nil {
		return ReplaceResponse{}, err
	}
	orderAttr := []any{logging.OrderID, req.Replace.OrderID.String()}

	acc, err := s.broker.OpenAccount(ctx, req.AccountID)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "replace requested", "open account failed", req.AccountID, err, orderAttr...)
		return ReplaceResponse{}, err
	}

	result, err := acc.Replace(ctx, req.Replace)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "replace requested", "replace failed", req.AccountID, err, orderAttr...)
		return ReplaceResponse{}, err
	}
	s.logOutcome(ctx, slog.LevelInfo, "replace requested", "replace failed", req.AccountID, nil,
		logging.OrderID, result.OrderID.String(), "status", result.Status.String())
	return ReplaceResponse{Result: result}, nil
}
