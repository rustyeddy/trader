package backtest

import (
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
)

// runPlanContext is the per-bar view the run loop hands to the Planner. It
// carries the account, the active exit/regime components, the current candle,
// and the configured execution-cost parameters.
type runPlanContext struct {
	acct      *execution.Account
	exit      strategy.ExitStrategy
	regime    strategy.RegimeFilter
	candle    market.CandleTime
	slippage  market.Price
	maxSpread market.Price
}

func (c runPlanContext) Account() *execution.Account   { return c.acct }
func (c runPlanContext) Exit() strategy.ExitStrategy   { return c.exit }
func (c runPlanContext) Regime() strategy.RegimeFilter { return c.regime }
func (c runPlanContext) Candle() market.CandleTime     { return c.candle }
func (c runPlanContext) Slippage() market.Price        { return c.slippage }
func (c runPlanContext) MaxSpread() market.Price       { return c.maxSpread }
