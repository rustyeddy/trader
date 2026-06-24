package backtest

import (
	"context"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
)

// fillAdjust returns the price adjustment for spread and slippage.
// Dukascopy OHLC prices are bid-side. When we are buying (long open, short
// close) we pay the ask: +spread+slippage. When selling we only lose slippage.
func fillAdjust(isBuy bool, spread, slippage market.Price) market.Price {
	if isBuy {
		return spread + slippage
	}
	return -slippage
}

// autoCloseExits checks every open lot against the bar's OHLC and
// immediately closes any that hit their stop or take-profit.
// It must be called before the strategy snapshot so the strategy only
// sees lots that are still open.
func autoCloseExits(ctx context.Context, b *execution.Broker, candle market.CandleTime, slippage market.Price) (int, error) {
	var hits []struct {
		lot    *execution.Lot
		exitPx market.Price
		reason string
		cause  execution.CloseCause
	}

	_ = b.Account.Lots.Range(func(lot *execution.Lot) error {
		if lot.State != execution.LotOpen {
			return nil
		}
		exitPx, reason, hit := checkExit(lot, candle.Candle)
		if !hit {
			return nil
		}
		cause := execution.CloseStopLoss
		if reason == "TAKE" {
			cause = execution.CloseTakeProfit
		}
		hits = append(hits, struct {
			lot    *execution.Lot
			exitPx market.Price
			reason string
			cause  execution.CloseCause
		}{lot, exitPx, reason, cause})
		return nil
	})

	for _, h := range hits {
		// Short closes by buying at ask; long closes by selling at bid.
		isBuy := h.lot.Side == market.Short
		adjPx := h.exitPx + fillAdjust(isBuy, candle.AvgSpread, slippage)
		cl := &execution.CloseRequest{
			Request: execution.Request{
				TradeCommon: h.lot.TradeCommon,
				RequestType: execution.RequestClose,
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
func checkExit(lot *execution.Lot, c market.Candle) (exitPx market.Price, reason string, hit bool) {
	if lot == nil {
		return 0, "", false
	}

	hasStop := lot.Stop != 0
	hasTake := lot.Take != 0

	switch lot.Side {
	case market.Long:
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

	case market.Short:
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
