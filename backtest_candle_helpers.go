package trader

import "context"

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
