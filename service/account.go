package service

import (
	"context"

	"github.com/rustyeddy/trader/brokers/oanda"
)

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
