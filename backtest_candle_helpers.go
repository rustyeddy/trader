package trader

import (
	"context"
	"fmt"
)

// fillAdjust returns the price adjustment for spread and slippage.
// Dukascopy OHLC prices are bid-side. When we are buying (long open, short
// close) we pay the ask: +spread+slippage. When selling we only lose slippage.
func fillAdjust(isBuy bool, spread, slippage Price) Price {
	if isBuy {
		return spread + slippage
	}
	return -slippage
}

// autoCloseExits checks every open lot against the bar's OHLC and
// immediately closes any that hit their stop or take-profit.
// It must be called before the strategy snapshot so the strategy only
// sees lots that are still open.
func autoCloseExits(ctx context.Context, b *Broker, candle CandleTime, slippage Price) (int, error) {
	var hits []struct {
		lot    *Lot
		exitPx Price
		reason string
		cause  closeCause
	}

	_ = b.Account.Lots.Range(func(lot *Lot) error {
		if lot.State != LotOpen {
			return nil
		}
		exitPx, reason, hit := checkExit(lot, candle.Candle)
		if !hit {
			return nil
		}
		cause := CloseStopLoss
		if reason == "TAKE" {
			cause = CloseTakeProfit
		}
		hits = append(hits, struct {
			lot    *Lot
			exitPx Price
			reason string
			cause  closeCause
		}{lot, exitPx, reason, cause})
		return nil
	})

	for _, h := range hits {
		// Short closes by buying at ask; long closes by selling at bid.
		isBuy := h.lot.Side == Short
		adjPx := h.exitPx + fillAdjust(isBuy, candle.AvgSpread, slippage)
		cl := &CloseRequest{
			Request: Request{
				TradeCommon: h.lot.TradeCommon,
				RequestType: RequestClose,
				Price:       adjPx,
				Timestamp:   candle.Timestamp,
				Reason:      h.reason,
			},
			Lot:        h.lot,
			CloseCause: h.cause,
		}
		if err := b.SubmitClose(ctx, cl); err != nil {
			return len(hits), err
		}
	}
	return len(hits), nil
}

// gapBarsSince returns the number of missing bars between prevTS and ts for
// the given timeframe. Returns 0 when prevTS is zero or when the gap is
// exactly one bar (the normal case).
func gapBarsSince(prevTS, ts Timestamp, tf Timeframe) int {
	if prevTS == 0 {
		return 0
	}

	delta := int64(ts - prevTS)
	if delta <= int64(tf) {
		return 0
	}

	return int(delta/int64(tf)) - 1
}

// closeLotAtPrice builds a Trade at the given price/timestamp and closes the
// lot via acct.CloseLot.
func closeLotAtPrice(acct *Account, lot *Lot, px Price, ts Timestamp) error {
	if acct == nil {
		return fmt.Errorf("nil account")
	}
	if lot == nil {
		return fmt.Errorf("nil position")
	}

	trade := &Trade{
		TradeCommon: lot.TradeCommon,
		EntryPrice:  lot.EntryPrice,
		EntryTime:   lot.EntryTime,
		ExitPrice:   px,
		ExitTime:    ts,
	}
	return acct.CloseLot(lot, trade)
}

// closeLotFromRequest closes a lot using the price and timestamp from cl,
// falling back to the candle's close price and timestamp when cl fields are zero.
func closeLotFromRequest(acct *Account, lot *Lot, cl *CloseRequest, fallback CandleTime) error {
	if cl == nil {
		return fmt.Errorf("nil close request")
	}

	px := cl.Price
	if px == 0 {
		px = fallback.Close
	}

	ts := cl.Timestamp
	if ts == 0 {
		ts = fallback.Timestamp
	}

	return closeLotAtPrice(acct, lot, px, ts)
}

// firstMatchingClose returns the first CloseRequest in plan that either
// targets current specifically or has no lot constraint (wildcard close).
// Returns nil when plan is empty or no match is found.
func firstMatchingClose(plan *StrategyPlan, current *Lot) *CloseRequest {
	if plan == nil || current == nil {
		return nil
	}

	for _, cl := range plan.Closes {
		if cl == nil {
			continue
		}
		if cl.Lot != nil && cl.Lot != current {
			continue
		}
		return cl
	}

	return nil
}

// firstOpenRequest returns the first OpenRequest from the plan, or nil if
// the plan is nil or has no opens.
func firstOpenRequest(plan *StrategyPlan) *OpenRequest {
	if plan == nil || len(plan.Opens) == 0 {
		return nil
	}
	return plan.Opens[0]
}

// ensureSizedOpenRequest calls SizePosition on req if Units is not already
// set. Requires a non-zero Stop price for risk-based sizing.
func ensureSizedOpenRequest(acct *Account, req *OpenRequest) error {
	if req == nil {
		return nil
	}
	if req.Units != 0 {
		return nil
	}
	if req.Stop == 0 {
		return fmt.Errorf("risk sizing requires a stop price")
	}
	return acct.SizePosition(req)
}

// forceLotCloseAtEnd closes a lot at the final candle's close price when the
// backtest period ends with open positions still held.
func forceLotCloseAtEnd(acct *Account, lot *Lot, lastC Candle, lastTS Timestamp) error {
	return closeLotAtPrice(acct, lot, lastC.Close, lastTS)
}

// checkExit evaluates stop/take on OHLC.
// If both stop & take hit in same bar, we assume stop-first (pessimistic).
func checkExit(lot *Lot, c Candle) (exitPx Price, reason string, hit bool) {
	if lot == nil {
		return 0, "", false
	}

	hasStop := lot.Stop != 0
	hasTake := lot.Take != 0

	switch lot.Side {
	case Long:
		stopHit := hasStop && c.Low <= lot.Stop
		takeHit := hasTake && c.High >= lot.Take
		if stopHit && takeHit {
			return lot.Stop, "STOP&TAKE same bar (stop-first)", true
		}
		if stopHit {
			return lot.Stop, "STOP", true
		}
		if takeHit {
			return lot.Take, "TAKE", true
		}

	case Short:
		stopHit := hasStop && c.High >= lot.Stop
		takeHit := hasTake && c.Low <= lot.Take
		if stopHit && takeHit {
			return lot.Stop, "STOP&TAKE same bar (stop-first)", true
		}
		if stopHit {
			return lot.Stop, "STOP", true
		}
		if takeHit {
			return lot.Take, "TAKE", true
		}
	}

	return 0, "", false
}
