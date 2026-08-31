package backtest

import (
	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/order"
)

// RunResponse is the structured result of the Run use case, mirroring
// the meaningful portions of backtest.Result — matching service/
// execution.SubmitResponse's own "mirror the domain result" convention
// (issue #221 review) rather than returning backtest.Result directly,
// so this service's public response shape does not change merely
// because backtest.Result's own internal shape does. These are Trader
// domain values, not a CLI- or JSON-specific DTO shape:
// report.NewBacktestReport (issue #220) remains the presentation
// boundary over a RunResponse's fields.
type RunResponse struct {
	Manifest    backtest.Manifest
	Account     account.Snapshot
	Trades      []order.Trade
	OpenTrades  []order.Trade
	EquityCurve []backtest.EquityPoint
	Metrics     backtest.Metrics
}

// toResponse converts a backtest.Result into a RunResponse, carrying
// over exactly the fields Result populated.
func toResponse(result backtest.Result) RunResponse {
	return RunResponse{
		Manifest:    result.Manifest,
		Account:     result.Account,
		Trades:      result.Trades,
		OpenTrades:  result.OpenTrades,
		EquityCurve: result.EquityCurve,
		Metrics:     result.Metrics,
	}
}
