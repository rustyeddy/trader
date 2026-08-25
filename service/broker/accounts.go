package broker

import (
	"context"
	"log/slog"

	"github.com/rustyeddy/trader/id"
)

// Accounts implements the read-only Accounts use case: every account
// reference the wrapped Broker currently knows about.
func (s *Service) Accounts(ctx context.Context, req AccountsRequest) (AccountsResponse, error) {
	refs, err := s.broker.Accounts(ctx)
	if err != nil {
		s.logOutcome(ctx, slog.LevelDebug, "accounts listed", "accounts list failed", id.AccountID{}, err)
		return AccountsResponse{}, err
	}
	s.logOutcome(ctx, slog.LevelDebug, "accounts listed", "accounts list failed", id.AccountID{}, nil,
		"account_count", len(refs))
	return AccountsResponse{Accounts: refs}, nil
}

// Snapshot implements the read-only Snapshot use case: req.AccountID's
// current observed state.
func (s *Service) Snapshot(ctx context.Context, req SnapshotRequest) (SnapshotResponse, error) {
	if err := req.Validate(); err != nil {
		return SnapshotResponse{}, err
	}

	acc, err := s.broker.OpenAccount(ctx, req.AccountID)
	if err != nil {
		s.logOutcome(ctx, slog.LevelDebug, "snapshot read", "open account failed", req.AccountID, err)
		return SnapshotResponse{}, err
	}

	snap, err := acc.Snapshot(ctx)
	s.logOutcome(ctx, slog.LevelDebug, "snapshot read", "snapshot read failed", req.AccountID, err)
	if err != nil {
		return SnapshotResponse{}, err
	}
	return SnapshotResponse{Snapshot: snap}, nil
}
