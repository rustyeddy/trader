package backtest

import (
	"context"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// autoCloseExits checks every open lot against the bar's OHLC and
// immediately closes any that hit their stop or take-profit.
// It must be called before the strategy snapshot so the strategy only
// sees lots that are still open.
func autoCloseExits(ctx context.Context, b *account.Ledger, candle market.Candle, slippage types.Price) (int, error) {
	var hits []struct {
		lot    *account.Lot
		exitPx types.Price
		reason string
		cause  account.CloseCause
	}

	_ = b.Account.Lots.Range(func(lot *account.Lot) error {
		if lot.State != account.LotOpen {
			return nil
		}
		exitPx, reason, hit := checkExit(lot, candle)
		if !hit {
			return nil
		}
		cause := account.CloseStopLoss
		if reason == "TAKE" {
			cause = account.CloseTakeProfit
		}
		hits = append(hits, struct {
			lot    *account.Lot
			exitPx types.Price
			reason string
			cause  account.CloseCause
		}{lot, exitPx, reason, cause})
		return nil
	})

	for _, h := range hits {
		// Short closes by buying at ask; long closes by selling at bid.
		isBuy := h.lot.Side == types.Short
		adjPx := h.exitPx + account.FillAdjust(isBuy, candle.AvgSpread, slippage)
		cl := &account.CloseRequest{
			Request: account.Request{
				TradeCommon: h.lot.TradeCommon,
				RequestType: account.RequestClose,
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
func checkExit(lot *account.Lot, c market.Candle) (exitPx types.Price, reason string, hit bool) {
	if lot == nil {
		return 0, "", false
	}

	hasStop := lot.Stop != 0
	hasTake := lot.Take != 0

	switch lot.Side {
	case types.Long:
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

	case types.Short:
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
