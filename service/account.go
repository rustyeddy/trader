package service

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/brokers/oanda"
)

// GetAccountSummary returns balance, NAV, margin, and unrealized P/L for
// the resolved account.
func (s *Service) GetAccountSummary(ctx context.Context) (*oanda.AccountSummary, error) {
	if err := s.ResolveAccount(ctx); err != nil {
		return nil, err
	}
	summary, err := s.OANDA.GetAccountSummary(ctx, s.AccountID)
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
func (s *Service) GetTransactions(ctx context.Context, sinceID int64) ([]oanda.Transaction, int64, error) {
	if err := s.ResolveAccount(ctx); err != nil {
		return nil, 0, err
	}
	return s.OANDA.GetTransactions(ctx, s.AccountID, sinceID)
}

// StreamTransactions opens a push subscription to the OANDA transaction
// stream. The returned channel closes when ctx is cancelled or the stream
// errors out (final event carries non-nil Err in the error case).
func (s *Service) StreamTransactions(ctx context.Context, opts oanda.StreamOptions) (<-chan oanda.TxEvent, error) {
	if err := s.ResolveAccount(ctx); err != nil {
		return nil, err
	}
	return s.OANDA.StreamTransactions(ctx, s.AccountID, opts)
}
