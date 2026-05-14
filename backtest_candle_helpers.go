package trader

import (
	"context"
	"fmt"
)

// autoCloseExits checks every open lot against the bar's OHLC and
// immediately closes any that hit their stop or take-profit.
// It must be called before the strategy snapshot so the strategy only
// sees lots that are still open.
func autoCloseExits(ctx context.Context, b *Broker, candle CandleTime) (int, error) {
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
		cl := &closeRequest{
			Request: Request{
				TradeCommon: h.lot.TradeCommon,
				RequestType: RequestClose,
				Price:       h.exitPx,
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

func closeLotFromRequest(acct *Account, lot *Lot, cl *closeRequest, fallback CandleTime) error {
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

func firstMatchingClose(plan *StrategyPlan, current *Lot) *closeRequest {
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

func firstOpenRequest(plan *StrategyPlan) *OpenRequest {
	if plan == nil || len(plan.Opens) == 0 {
		return nil
	}
	return plan.Opens[0]
}

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
