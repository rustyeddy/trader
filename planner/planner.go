// Package planner turns a strategy's intent into concrete, finalized broker
// requests. It owns the business logic that sits between signal generation
// (strategy) and execution (engine/broker): the regime and max-spread gates,
// fill-price adjustment, initial-stop placement, and position sizing.
//
// Strategies emit strategy.Signal values; PlanSignal converts them into
// StrategyPlans that the engine submits to the broker.
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
	Instrument() string
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

// DefaultPlanner is the behavior-preserving extraction of the logic that used
// to live inline in the backtest run loop. It is stateless.
type DefaultPlanner struct{}

// finalize applies the regime and max-spread gates to the plan's opens, then
// for each surviving open resolves the fill price (spread+slippage), the
// initial stop, and the position size. Strategy-driven closes get their fill
// price resolved too. The returned plan is ready to submit to the broker as-is;
// the input plan is mutated in place and also returned for convenience.
func (DefaultPlanner) finalize(raw *strategy.StrategyPlan, pc PlanContext) (*strategy.StrategyPlan, Stats, error) {
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

// PlanSignal translates a strategy.Signal into a finalized StrategyPlan using
// the same gates and order-construction logic as Plan.
//
//   - Flat + !CloseAll → hold; return empty plan immediately.
//   - CloseAll=true → close ALL open lots (time-based / band-reversion exits).
//   - Directional + !CloseAll → reversal-close opposing lots only.
//   - Directional side → open a new position at candle close; then run the
//     full Plan pipeline (regime gate, max-spread gate, fill-price, sizing).
func (p DefaultPlanner) PlanSignal(sig strategy.Signal, pc PlanContext) (*strategy.StrategyPlan, Stats, error) {
	plan := &strategy.StrategyPlan{Reason: sig.Reason}

	if sig.Side == market.Flat && !sig.CloseAll {
		return plan, Stats{}, nil
	}

	candle := pc.Candle()
	acct := pc.Account()

	if sig.CloseAll && acct != nil {
		// Close ALL open lots — strategy-controlled exit (not just reversal).
		_ = acct.Lots.Range(func(lot *execution.Lot) error {
			if lot.State != execution.LotOpen {
				return nil
			}
			plan.Closes = append(plan.Closes, &execution.CloseRequest{
				Request: execution.Request{
					TradeCommon: lot.TradeCommon,
					Reason:      sig.Reason,
					Candle:      candle.Candle,
					RequestType: execution.RequestClose,
					Price:       candle.Close,
					Timestamp:   candle.Timestamp,
				},
				Lot:        lot,
				CloseCause: execution.CloseManual,
			})
			return nil
		})
	} else if sig.Side != market.Flat && acct != nil {
		// Reversal-close: close any open lots on the opposing side only.
		_ = acct.Lots.Range(func(lot *execution.Lot) error {
			if lot.State != execution.LotOpen || lot.Side == sig.Side {
				return nil
			}
			plan.Closes = append(plan.Closes, &execution.CloseRequest{
				Request: execution.Request{
					TradeCommon: lot.TradeCommon,
					Reason:      "signal-reverse",
					Candle:      candle.Candle,
					RequestType: execution.RequestClose,
					Price:       candle.Close,
					Timestamp:   candle.Timestamp,
				},
				Lot:        lot,
				CloseCause: execution.CloseManual,
			})
			return nil
		})
	}

	if sig.Side != market.Flat {
		plan.Opens = append(plan.Opens, execution.NewOpenRequest(
			pc.Instrument(), &candle, sig.Side, sig.Stop, 0, sig.Reason,
		))
	}

	return p.finalize(plan, pc)
}
