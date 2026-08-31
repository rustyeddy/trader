package backtest

import (
	"context"

	"github.com/rustyeddy/trader/logging"
)

// logRunOutcome logs Run's own outcome: Info with the resulting
// RunID/trade counts on success, Error with the request's own
// identifying attributes otherwise. Matches service/execution's own
// "log the operation boundary once" convention.
func (s *Service) logRunOutcome(ctx context.Context, req RunRequest, resp RunResponse, err error) {
	if err != nil {
		strategyName := ""
		if req.Strategy != nil {
			strategyName = req.Strategy.Describe().Name
		}
		s.logger.ErrorContext(ctx, "backtest run failed", "strategy_name", strategyName, "error", err)
		return
	}
	s.logger.InfoContext(ctx, "backtest run completed",
		logging.RunID, resp.Manifest.RunID().String(),
		"strategy_name", resp.Manifest.StrategyName(),
		"closed_trades", len(resp.Trades),
		"open_trades", len(resp.OpenTrades),
	)
}
