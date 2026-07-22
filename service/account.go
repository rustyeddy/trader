package service

import (
	"context"

	"github.com/rustyeddy/trader/brokers/oanda"
)

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
