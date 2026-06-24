// Package planner turns a strategy's intent into concrete, finalized broker
// requests. It owns the business logic that sits between signal generation
// (strategy) and execution (engine/broker): the regime and max-spread gates,
// fill-price adjustment, initial-stop placement, and position sizing.
//
// Today a Planner finalizes a strategy.StrategyPlan in place; the eventual shape
// is strategy -> Signal -> Planner -> Trader, where strategies emit pure signals
// and the planner constructs the orders. This package is the seam that makes
// that flip possible without disturbing the engine.
package planner

import (
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
)

// PlanContext is the read/compute view a Planner needs to finalize a plan
// against the current bar: the account (for sizing), the active exit strategy
// and regime filter, the current candle, and the configured execution-cost
// parameters.
type PlanContext interface {
	Account() *execution.Account
	Exit() strategy.ExitStrategy
	Regime() strategy.RegimeFilter
	Candle() market.CandleTime
	Slippage() market.Price
	MaxSpread() market.Price
}

// Stats reports the execution-cost bookkeeping a Planner produces while
// finalizing a plan. Callers fold these into their run state.
type Stats struct {
	SpreadFiltered int          // opens suppressed by the max-spread gate
	SpreadOpened   int          // opens accepted (denominator for avg-spread)
	SpreadSum      market.Price // sum of candle AvgSpread over accepted opens
}

// Planner finalizes a raw strategy plan into broker-ready requests.
type Planner interface {
	// Plan applies the regime and max-spread gates to the plan's opens, then for
	// each surviving open resolves the fill price (spread+slippage), the initial
	// stop, and the position size. Strategy-driven closes get their fill price
	// resolved too. The returned plan is ready to submit to the broker as-is; the
	// input plan is mutated in place and also returned for convenience.
	Plan(raw *strategy.StrategyPlan, pc PlanContext) (*strategy.StrategyPlan, Stats, error)
}

// DefaultPlanner is the behavior-preserving extraction of the logic that used
// to live inline in the backtest run loop. It is stateless.
type DefaultPlanner struct{}

// Plan implements Planner. The order of operations mirrors the historical run
// loop exactly so backtest results stay byte-identical: regime gate, then
// max-spread gate, then per-open fill/stop/size, with strategy closes adjusted
// last. Counters are returned rather than mutated on the caller's state.
func (DefaultPlanner) Plan(raw *strategy.StrategyPlan, pc PlanContext) (*strategy.StrategyPlan, Stats, error) {
	var stats Stats
	if raw == nil || pc == nil {
		return raw, stats, nil
	}

	candle := pc.Candle()
	slippage := pc.Slippage()
	maxSpread := pc.MaxSpread()
	exit := pc.Exit()
	regime := pc.Regime()
	acct := pc.Account()

	// Regime filter: suppress new entries in ranging/consolidating markets.
	// Existing positions continue to be managed by the exit strategy.
	if regime != nil && regime.Ready() {
		if !regime.Trending() {
			raw.Opens = nil
		} else if len(raw.Opens) > 0 {
			filtered := raw.Opens[:0]
			for _, o := range raw.Opens {
				if regime.AllowSide(o.Side) {
					filtered = append(filtered, o)
				}
			}
			raw.Opens = filtered
		}
	}

	// Max-spread filter: skip entries when the bid-ask spread is too wide
	// (market opens, news events, low-liquidity periods).
	if maxSpread > 0 && candle.AvgSpread > maxSpread && len(raw.Opens) > 0 {
		stats.SpreadFiltered++
		raw.Opens = nil
	}

	// Strategy-driven closes: short closes by buying at ask, long closes by
	// selling at bid.
	for _, cl := range raw.Closes {
		if cl != nil && cl.Lot != nil {
			isBuy := cl.Lot.Side == market.Short
			cl.Price += execution.FillAdjust(isBuy, candle.AvgSpread, slippage)
		}
	}

	// Opens: resolve fill price, initial stop, and size.
	for _, openReq := range raw.Opens {
		if openReq == nil {
			continue
		}

		// Long buys at ask; short sells at bid.
		isBuy := openReq.Side == market.Long
		openReq.Price += execution.FillAdjust(isBuy, candle.AvgSpread, slippage)
		stats.SpreadOpened++
		stats.SpreadSum += candle.AvgSpread

		// Let the exit strategy override the initial stop when configured.
		if exit != nil && exit.Ready() {
			if s := exit.InitialStop(openReq.Side, openReq.Price, candle.Candle); s != 0 {
				openReq.Stop = s
			}
		}

		if openReq.Units == 0 && acct != nil {
			if err := acct.SizePosition(openReq); err != nil {
				return raw, stats, err
			}
		}
	}

	return raw, stats, nil
}
