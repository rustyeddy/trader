package broker

import (
	"context"
	"log/slog"
)

// Submit implements the mutating Submit use case: ask req.AccountID's
// broker to accept req.Order as a new order.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) (SubmitResponse, error) {
	if err := req.Validate(); err != nil {
		return SubmitResponse{}, err
	}

	acc, err := s.broker.OpenAccount(ctx, req.AccountID)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "order submitted", "open account failed", req.AccountID, err)
		return SubmitResponse{}, err
	}

	o, err := acc.Submit(ctx, req.Order)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "order submitted", "order submit failed", req.AccountID, err)
		return SubmitResponse{}, err
	}
	s.logOutcome(ctx, slog.LevelInfo, "order submitted", "order submit failed", req.AccountID, nil,
		"order_id", o.Request.OrderID.String(), "status", o.Status.String())
	return SubmitResponse{Order: o}, nil
}

// Cancel implements the mutating Cancel use case: ask req.AccountID's
// broker to cancel an existing order.
func (s *Service) Cancel(ctx context.Context, req CancelRequest) (CancelResponse, error) {
	if err := req.Validate(); err != nil {
		return CancelResponse{}, err
	}

	acc, err := s.broker.OpenAccount(ctx, req.AccountID)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "cancel requested", "open account failed", req.AccountID, err)
		return CancelResponse{}, err
	}

	result, err := acc.Cancel(ctx, req.Cancel)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "cancel requested", "cancel failed", req.AccountID, err)
		return CancelResponse{}, err
	}
	s.logOutcome(ctx, slog.LevelInfo, "cancel requested", "cancel failed", req.AccountID, nil,
		"order_id", result.OrderID.String(), "status", result.Status.String())
	return CancelResponse{Result: result}, nil
}

// Replace implements the mutating Replace use case: ask req.AccountID's
// broker to modify an existing order's quantity and/or prices in
// place.
func (s *Service) Replace(ctx context.Context, req ReplaceRequest) (ReplaceResponse, error) {
	if err := req.Validate(); err != nil {
		return ReplaceResponse{}, err
	}

	acc, err := s.broker.OpenAccount(ctx, req.AccountID)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "replace requested", "open account failed", req.AccountID, err)
		return ReplaceResponse{}, err
	}

	result, err := acc.Replace(ctx, req.Replace)
	if err != nil {
		s.logOutcome(ctx, slog.LevelInfo, "replace requested", "replace failed", req.AccountID, err)
		return ReplaceResponse{}, err
	}
	s.logOutcome(ctx, slog.LevelInfo, "replace requested", "replace failed", req.AccountID, nil,
		"order_id", result.OrderID.String(), "status", result.Status.String())
	return ReplaceResponse{Result: result}, nil
}
