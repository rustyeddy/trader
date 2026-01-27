package cmd

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/indicators"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/risk"
)

// EmaCrossStrategy trades a single instrument using a fast/slow EMA crossover.
type EmaCrossStrategy struct {
	Instrument string

	FastPeriod int
	SlowPeriod int

	RiskPct  float64
	StopPips float64
	RR       float64

	fast *indicators.ExponentialMA
	slow *indicators.ExponentialMA

	lastDiff     float64
	haveLastDiff bool

	openTradeID string
	openUnits   float64
}

func NewEmaCrossStrategy(instrument string, fast, slow int, riskPct, stopPips, rr float64) *EmaCrossStrategy {
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

	if !s.fast.Ready() || !s.slow.Ready() {
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

	switch {
	case bullCross:
		return s.onSignal(ctx, b, tick.Time, "BullCross", +1)
	case bearCross:
		return s.onSignal(ctx, b, tick.Time, "BearCross", -1)
	default:
		return nil
	}
}

func (s *EmaCrossStrategy) onSignal(
	ctx context.Context,
	b broker.Broker,
	now time.Time,
	signal string,
	dir int,
) error {
	if s.openTradeID != "" {
		if (s.openUnits > 0 && dir > 0) || (s.openUnits < 0 && dir < 0) {
			return nil
		}

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

		s.openTradeID = ""
		s.openUnits = 0
	}

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
