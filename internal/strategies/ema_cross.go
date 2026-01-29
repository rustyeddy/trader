package strategies

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/indicators"
	"github.com/rustyeddy/trader/internal/risk"
	"github.com/rustyeddy/trader/market"
)

// EmaCrossStrategy trades a single instrument using a fast/slow EMA crossover.
// - Enters only on cross
// - Reverses on opposite cross (close then open)
// - Uses risk.Calculate for sizing (fixed stop in pips)
// - Exit reasons recorded in journal via CloseTrade(reason)
type EmaCrossStrategy struct {
	Instrument string

	FastPeriod int // 20
	SlowPeriod int // 50

	RiskPct  float64 // 0.005 (0.5%)
	StopPips float64 // e.g. 20
	RR       float64 // take-profit multiple of risk, e.g. 2.0

	fast *indicators.ExponentialMA
	slow *indicators.ExponentialMA

	lastDiff     float64
	haveLastDiff bool

	openTradeID string
	openUnits   float64 // >0 long, <0 short
}

func NewEmaCross(instrument string, fast, slow int, riskPct, stopPips, rr float64) *EmaCrossStrategy {
	if rr <= 0 {
		rr = 2.0
	}
	return &EmaCrossStrategy{
		Instrument: instrument,

		FastPeriod: fast,
		SlowPeriod: slow,

		RiskPct:  riskPct,
		StopPips: stopPips,
		RR:       rr,

		fast: indicators.NewEMA(fast),
		slow: indicators.NewEMA(slow),
	}
}

func (s *EmaCrossStrategy) OnTick(ctx context.Context, b broker.Broker, tick broker.Price) error {
	if tick.Instrument != s.Instrument {
		return nil
	}

	// Treat each tick as a "candle" with OHLC = mid; good enough for first pass.
	mid := tick.Mid()
	c := market.Candle{
		Open:  mid,
		High:  mid,
		Low:   mid,
		Close: mid,
		Time:  tick.Time,
	}

	s.fast.Update(c)
	s.slow.Update(c)

	// Wait until both EMAs are warmed up.
	if !s.fast.Ready() || !s.slow.Ready() {
		return nil
	}

	diff := s.fast.Value() - s.slow.Value()

	// Need a previous diff to detect a cross.
	if !s.haveLastDiff {
		s.lastDiff = diff
		s.haveLastDiff = true
		return nil
	}

	// Cross logic:
	// - Bull cross: diff goes from <=0 to >0
	// - Bear cross: diff goes from >=0 to <0
	bullCross := diff > 0 && s.lastDiff <= 0
	bearCross := diff < 0 && s.lastDiff >= 0

	// Update lastDiff early/always to avoid repeated triggers if we return.
	s.lastDiff = diff

	switch {
	case bullCross:
		return s.onSignal(ctx, b, tick.Time, "BullCross", +1)
	case bearCross:
		return s.onSignal(ctx, b, tick.Time, "BearCross", -1)
	default:
		return nil
	}
}

func (s *EmaCrossStrategy) onSignal(ctx context.Context,
	b broker.Broker,
	now time.Time,
	signal string,
	dir int) error { // +1 long, -1 short

	// If we already have a position in the same direction, do nothing (enter only on cross).
	if s.openTradeID != "" {
		if (s.openUnits > 0 && dir > 0) || (s.openUnits < 0 && dir < 0) {
			return nil
		}

		// Opposite cross: exit existing position with a clean reason.
		closer, ok := b.(interface {
			CloseTrade(context.Context, string, string) error
		})
		if !ok {
			return fmt.Errorf("ema-cross: broker does not support CloseTrade; need *sim.Engine")
		}

		exitReason := "ExitOn" + signal
		if err := closer.CloseTrade(ctx, s.openTradeID, exitReason); err != nil {
			return err
		}

		// Clear position state
		s.openTradeID = ""
		s.openUnits = 0
	}

	// Enter new position (only because we got a cross).
	return s.openPosition(ctx, b, now, signal, dir)
}

func (s *EmaCrossStrategy) openPosition(ctx context.Context, b broker.Broker, now time.Time, signal string, dir int) error {
	acct, err := b.GetAccount(ctx)
	if err != nil {
		return err
	}

	px, err := b.GetPrice(ctx, s.Instrument)
	if err != nil {
		return err
	}

	meta, ok := market.Instruments[s.Instrument]
	if !ok {
		return fmt.Errorf("ema-cross: unknown instrument %q in market.Instruments", s.Instrument)
	}

	quoteToAccount, err := market.QuoteToAccountRate(s.Instrument, acct.Currency, b)
	if err != nil {
		return err
	}

	pip := risk.PipSize(meta.PipLocation)
	if pip <= 0 {
		return fmt.Errorf("ema-cross: bad pip size for pipLocation=%d", meta.PipLocation)
	}

	entry := px.Ask
	if dir < 0 {
		entry = px.Bid
	}

	stopDist := s.StopPips * pip
	if stopDist <= 0 || math.IsNaN(stopDist) || math.IsInf(stopDist, 0) {
		return fmt.Errorf("ema-cross: invalid stop distance (StopPips=%v pip=%v)", s.StopPips, pip)
	}

	var stop, tp float64
	if dir > 0 {
		stop = entry - stopDist
		tp = entry + (entry-stop)*s.RR
	} else {
		stop = entry + stopDist
		tp = entry - (stop-entry)*s.RR
	}

	size := risk.Calculate(risk.Inputs{
		Equity:         acct.Equity,
		RiskPct:        s.RiskPct,
		EntryPrice:     entry,
		StopPrice:      stop,
		PipLocation:    meta.PipLocation,
		QuoteToAccount: quoteToAccount,
	})

	units := size.Units
	if units <= 0 {
		return fmt.Errorf("ema-cross: calculated non-positive units (%v)", units)
	}
	if dir < 0 {
		units = -units
	}

	// Attach SL/TP so the engine can auto-close and journal StopLoss/TakeProfit reasons.
	req := broker.MarketOrderRequest{
		Instrument: s.Instrument,
		Units:      units,
		StopLoss:   &stop,
		TakeProfit: &tp,
	}

	fill, err := b.CreateMarketOrder(ctx, req)
	if err != nil {
		return err
	}

	s.openTradeID = fill.TradeID
	s.openUnits = units

	// Entry "reason" — journal currently records exit reasons, so we print the entry reason.
	fmt.Printf(
		"%s ENTRY %s %s units=%.0f entry=%.5f stop=%.5f tp=%.5f riskPct=%.4f stopPips=%.1f riskAmt=%.2f\n",
		now.UTC().Format(time.RFC3339),
		s.Instrument,
		signal,
		units,
		entry,
		stop,
		tp,
		s.RiskPct,
		s.StopPips,
		size.RiskAmount,
	)

	return nil
}

// OnTradeClosed implements TradeClosedListener.
// It's called by the engine when a trade is auto-closed (e.g., stop loss or take profit).
// Note: This method is not safe for concurrent use. Strategies should only be used
// in a single-threaded context (e.g., in the backtest runner's sequential loop).
func (s *EmaCrossStrategy) OnTradeClosed(tradeID string, reason string) {
	// If this was our open trade, clear our internal state
	if s.openTradeID == tradeID {
		s.openTradeID = ""
		s.openUnits = 0
	}
}
