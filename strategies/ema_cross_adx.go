package strategies

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/market/indicators"
	"github.com/rustyeddy/trader/risk"
	"github.com/rustyeddy/trader/sim"
)

// EmaCrossStrategy trades a single instrument using a fast/slow EMA crossover.
// - Enters only on cross
// - Reverses on opposite cross (close then open)
// - Uses risk.Calculate for sizing (fixed stop in pips)
// - Exit reasons recorded in journal via CloseTrade(reason)
type EMAADX struct {
	*EMAADXConfig

	fast *indicators.EMA
	slow *indicators.EMA
	adx  *indicators.ADX

	pendingDir    int
	pendingSignal string

	lastDiff     float64
	haveLastDiff bool

	openTradeID string
	openUnits   float64 // >0 long, <0 short
}

type EMAADXConfig struct {
	FastPeriod int `json:"fast-period"` // 20
	SlowPeriod int `json:"slow-period"` // 50

	Instrument string  `json:"instrument"`
	RiskPct    float64 `json:"risk-percent"` // 0.005 (0.5%)
	StopPips   float64 `json:"stopPips"`     // e.g. 20
	RR         float64 `json:"risk-reward"`  // take-profit multiple of risk, e.g. 2.0
}

func (e *EMAADXConfig) JSON() ([]byte, error) {
	return json.Marshal(e)
}

func EMAADXConfigDefaults() *EMAADXConfig {
	return &EMAADXConfig{
		Instrument: "EUR_USD",
		FastPeriod: 10,
		SlowPeriod: 30,
		RiskPct:    0.005,
		StopPips:   20,
		RR:         2.0,
	}
}

func NewEMAADX(cfg *EMAADXConfig) *EMAADX {
	if cfg.RR <= 0 {
		cfg.RR = 2.0
	}
	return &EMAADX{
		EMAADXConfig: cfg,
		fast:         indicators.NewEMA(cfg.FastPeriod),
		slow:         indicators.NewEMA(cfg.SlowPeriod),
		adx:          indicators.NewADX(14),
	}
}

// syncOpenState clears strategy position state if the engine has already closed the trade
// (e.g. StopLoss/TakeProfit).
func (s *EMAADX) syncOpenState(b broker.Broker) {
	if s.openTradeID == "" {
		return
	}
	// Only the sim engine currently knows trade state; if the broker doesn't support it,
	// we fall back to best-effort close handling.
	if eng, ok := b.(interface{ IsTradeOpen(string) bool }); ok {
		if !eng.IsTradeOpen(s.openTradeID) {
			s.openTradeID = ""
			s.openUnits = 0
		}
	}
}

func (s *EMAADX) OnCandle(ctx context.Context, b broker.Broker, c market.Candle) error {
	s.fast.Update(c)
	s.slow.Update(c)
	s.adx.Update(c) // <-- real ADX

	if !s.fast.Ready() || !s.slow.Ready() || !s.adx.Ready() {
		return nil
	}

	diff := s.fast.Value() - s.slow.Value()

	if !s.haveLastDiff {
		s.lastDiff = diff
		s.haveLastDiff = true
		return nil
	}

	bullCross := diff > 0 && s.lastDiff <= 0
	bearCross := diff < 0 && s.lastDiff >= 0

	s.lastDiff = diff

	// ADX regime filter
	if s.adx.Value() < 25 {
		return nil
	}

	switch {
	case bullCross:
		s.pendingDir = +1
		s.pendingSignal = "BullCrossADX"
	case bearCross:
		s.pendingDir = -1
		s.pendingSignal = "BearCrossADX"
	}

	return nil
}

func (s *EMAADX) OnTick(ctx context.Context, b broker.Broker, tick market.Tick) error {
	if tick.Instrument != s.Instrument {
		return nil
	}

	if s.pendingDir == 0 {
		return nil
	}

	dir := s.pendingDir
	signal := s.pendingSignal

	s.pendingDir = 0
	s.pendingSignal = ""

	return s.onSignal(ctx, b, tick.Time, signal, dir)
}

func (s *EMAADX) onSignal(ctx context.Context,
	b broker.Broker,
	now time.Time,
	signal string,
	dir int) error { // +1 long, -1 short

	// Engine may have auto-closed our trade via StopLoss/TakeProfit.
	s.syncOpenState(b)

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
			// The engine may have already closed the trade (StopLoss/TakeProfit),
			// so treat that as a no-op and just sync our state below.
			if errors.Is(err, sim.ErrTradeAlreadyClosed) || errors.Is(err, sim.ErrTradeNotFound) {
				// No-op: state will be cleared unconditionally after this block.
			} else {
				return err
			}
		}

		// Clear position state
		s.openTradeID = ""
		s.openUnits = 0
	}

	// Enter new position (only because we got a cross).
	return s.openPosition(ctx, b, now, signal, dir)
}

func (s *EMAADX) openPosition(ctx context.Context, b broker.Broker, now time.Time, signal string, dir int) error {
	acct, err := b.GetAccount(ctx)
	if err != nil {
		return err
	}

	px, err := b.GetTick(ctx, s.Instrument)
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
	verbose := true
	if verbose {
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
	}
	return nil
}
