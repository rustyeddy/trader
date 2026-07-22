package service

import (
	"context"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers/oanda"
)

// ── default-account back-compat wrappers ─────────────────────────────────────

// PlaceMarketOrder runs the risk-sized order workflow on the default account.
func (s *Service) PlaceMarketOrder(ctx context.Context, req account.PlaceMarketOrderRequest) (*account.PlaceMarketOrderResult, error) {
	acc, err := s.DefaultAccount(ctx)
	if err != nil {
		return nil, err
	}
	return acc.PlaceMarketOrder(ctx, req)
}

// CloseTrade closes a trade on the default account.
func (s *Service) CloseTrade(ctx context.Context, tradeID string, units int64) (*oanda.CloseTradeResult, error) {
	acc, err := s.DefaultAccount(ctx)
	if err != nil {
		return nil, err
	}
	return acc.CloseTrade(ctx, tradeID, units)
}

// UpdateTradeStop updates a stop/take-profit on the default account.
func (s *Service) UpdateTradeStop(ctx context.Context, tradeID string, stopPx, takePx float64) error {
	acc, err := s.DefaultAccount(ctx)
	if err != nil {
		return err
	}
	return acc.UpdateTradeStop(ctx, tradeID, stopPx, takePx)
}

// ListOpenTrades returns the open positions on the first/default account (read).
func (s *Service) ListOpenTrades(ctx context.Context) ([]oanda.OpenTrade, error) {
	acc, err := s.FirstAccount(ctx)
	if err != nil {
		return nil, err
	}
	return acc.ListOpenTrades(ctx)
}
