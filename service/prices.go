package service

import (
	"context"

	"github.com/rustyeddy/trader/account"
)

// GetPrices fetches current bid/ask snapshots. Prices are market data, not
// account-specific, so this read may default to the first account.
func (s *Service) GetPrices(ctx context.Context, req account.GetPricesRequest) ([]account.PriceInfo, error) {
	acc, err := s.FirstAccount(ctx)
	if err != nil {
		return nil, err
	}
	return acc.GetPrices(ctx, req)
}
