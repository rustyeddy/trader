package service

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/account"
)

// RunLiveStrategy runs a live strategy loop on the default account. Config is
// validated (and the OANDA client checked) before the account is resolved so
// callers see precise errors without a network round-trip.
func (s *Service) RunLiveStrategy(ctx context.Context, cfg account.LiveRunConfig) error {
	if cfg.Strategy == nil {
		return fmt.Errorf("live runner: strategy is required")
	}
	if cfg.Instrument == "" {
		return fmt.Errorf("live runner: instrument is required")
	}
	if s.OANDA == nil {
		return fmt.Errorf("live runner: OANDA client not configured")
	}
	acc, err := s.DefaultAccount(ctx)
	if err != nil {
		return fmt.Errorf("live runner: %w", err)
	}
	return acc.RunLiveStrategy(ctx, cfg)
}
