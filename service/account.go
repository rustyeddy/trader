package service

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/brokers/oanda"
)

// GetAccountSummary returns balance, NAV, margin, and unrealized P/L.
// When the account snapshot is running it reads from the local cache;
// otherwise it falls back to a direct OANDA REST call.
func (a *Account) GetAccountSummary(ctx context.Context) (*oanda.AccountSummary, error) {
	if snap := a.getSnapshot(); snap != nil {
		return snap.Summary(), nil
	}
	summary, err := a.broker().GetAccountSummary(ctx, a.ID)
	if err != nil {
		return nil, fmt.Errorf("get account summary: %w", err)
	}
	return summary, nil
}

// GetTransactions polls for transactions with ID > sinceID. Returns the
// transactions and the new lastTransactionID for the next poll.
//
// OANDA caps responses at 1000; if you get back exactly 1000, call again
// with the new lastID.
func (a *Account) GetTransactions(ctx context.Context, sinceID int64) ([]oanda.Transaction, int64, error) {
	return a.broker().GetTransactions(ctx, a.ID, sinceID)
}

// StreamTransactions opens a push subscription to the OANDA transaction
// stream. The returned channel closes when ctx is cancelled or the stream
// errors out (final event carries non-nil Err in the error case).
func (a *Account) StreamTransactions(ctx context.Context, opts oanda.StreamOptions) (<-chan oanda.TxEvent, error) {
	return a.broker().StreamTransactions(ctx, a.ID, opts)
}

// GetAccountSummary returns the summary for the first/default account. This
// is a read, so it may default to the first account (see FirstAccount).
func (s *Service) GetAccountSummary(ctx context.Context) (*oanda.AccountSummary, error) {
	acc, err := s.FirstAccount(ctx)
	if err != nil {
		return nil, err
	}
	return acc.GetAccountSummary(ctx)
}

// GetTransactions polls transactions on the first/default account (read).
func (s *Service) GetTransactions(ctx context.Context, sinceID int64) ([]oanda.Transaction, int64, error) {
	acc, err := s.FirstAccount(ctx)
	if err != nil {
		return nil, 0, err
	}
	return acc.GetTransactions(ctx, sinceID)
}

// StreamTransactions opens a transaction stream on the first/default account (read).
func (s *Service) StreamTransactions(ctx context.Context, opts oanda.StreamOptions) (<-chan oanda.TxEvent, error) {
	acc, err := s.FirstAccount(ctx)
	if err != nil {
		return nil, err
	}
	return acc.StreamTransactions(ctx, opts)
}
