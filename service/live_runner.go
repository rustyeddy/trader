package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/brokers/oanda"
)

// LiveRunConfig controls a single live strategy run.
type LiveRunConfig struct {
	// Instrument is the OANDA instrument name, e.g. "EUR_USD".
	Instrument string

	// TickInterval is how often the runner polls for prices and ticks the
	// strategy. Defaults to 60s when zero.
	TickInterval time.Duration

	// Strategy is the live strategy to run. Required.
	Strategy trader.LiveStrategy

	// RiskPct is the default risk per trade when the strategy's
	// LiveOpenRequest carries zero. Defaults to 0.1.
	RiskPct float64

	// MaxUnits caps position size in units (absolute). 0 = no cap.
	MaxUnits int64

	// MaxPositionUSD caps position notional value in account currency. 0 = no cap.
	MaxPositionUSD float64
}

// RunLiveStrategy runs a live strategy loop until ctx is cancelled.
// On each tick it:
//  1. Fetches the current bid/ask price.
//  2. Queries open trades from the broker and increments their tick counter.
//  3. Calls strategy.Tick to get a plan.
//  4. Executes closes, then the open (if any).
func (s *Service) RunLiveStrategy(ctx context.Context, cfg LiveRunConfig) error {
	if cfg.Strategy == nil {
		return fmt.Errorf("live runner: strategy is required")
	}
	if cfg.Instrument == "" {
		return fmt.Errorf("live runner: instrument is required")
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = 60 * time.Second
	}
	if cfg.RiskPct <= 0 {
		cfg.RiskPct = 0.1
	}
	if s.OANDA == nil {
		return fmt.Errorf("live runner: OANDA client not configured")
	}
	if err := s.ResolveAccount(ctx); err != nil {
		return fmt.Errorf("live runner: %w", err)
	}

	log := s.Log
	if log == nil {
		log = slog.Default()
	}

	log.Info("live runner: starting",
		"strategy", cfg.Strategy.Name(),
		"instrument", cfg.Instrument,
		"tick_interval", cfg.TickInterval,
	)

	// tickCounts tracks how many ticks each open trade has been held.
	// This is maintained by the runner so the strategy doesn't need to.
	tickCounts := map[string]int{}

	ticker := time.NewTicker(cfg.TickInterval)
	defer ticker.Stop()

	marketWasClosed := false

	tick := func() {
		if trader.IsForexMarketClosed(time.Now()) {
			if !marketWasClosed {
				log.Info("live runner: market closed, pausing", "instrument", cfg.Instrument)
				marketWasClosed = true
			}
			return
		}
		if marketWasClosed {
			log.Info("live runner: market open, resuming", "instrument", cfg.Instrument)
			marketWasClosed = false
		}
		if err := s.runOneTick(ctx, cfg, tickCounts, log); err != nil {
			log.Warn("live runner: tick error", "err", err)
		}
	}

	// Run the first tick immediately so there's no initial delay.
	tick()

	for {
		select {
		case <-ctx.Done():
			log.Info("live runner: stopped", "strategy", cfg.Strategy.Name())
			return nil
		case <-ticker.C:
			tick()
		}
	}
}

func (s *Service) runOneTick(
	ctx context.Context,
	cfg LiveRunConfig,
	tickCounts map[string]int,
	log *slog.Logger,
) error {
	// 1. Current price.
	prices, err := s.OANDA.GetPricing(ctx, s.AccountID, cfg.Instrument)
	if err != nil {
		return fmt.Errorf("get pricing: %w", err)
	}
	if len(prices) == 0 {
		return fmt.Errorf("no price for %s", cfg.Instrument)
	}
	px := prices[0]
	livePrice := trader.LivePrice{
		Instrument: cfg.Instrument,
		Bid:        px.Bid,
		Ask:        px.Ask,
		Time:       time.Now(),
	}

	// 2. Open trades on the account, filtered to this instrument.
	allTrades, err := s.OANDA.GetOpenTrades(ctx, s.AccountID)
	if err != nil {
		return fmt.Errorf("get open trades: %w", err)
	}
	inst := normalizeInstrument(cfg.Instrument)
	var liveTrades []trader.LiveTrade
	seenIDs := map[string]struct{}{}
	for _, t := range allTrades {
		if normalizeInstrument(t.Instrument) != inst {
			continue
		}
		seenIDs[t.ID] = struct{}{}
		tickCounts[t.ID]++
		liveTrades = append(liveTrades, trader.LiveTrade{
			ID:           t.ID,
			Instrument:   t.Instrument,
			Units:        t.Units,
			EntryPrice:   t.EntryPrice,
			UnrealizedPL: t.UnrealizedPL,
			TicksOpen:    tickCounts[t.ID],
		})
	}
	// Prune closed trades from the tick counter.
	for id := range tickCounts {
		if _, ok := seenIDs[id]; !ok {
			delete(tickCounts, id)
		}
	}

	log.Info("live runner: tick",
		"strategy", cfg.Strategy.Name(),
		"instrument", cfg.Instrument,
		"bid", px.Bid, "ask", px.Ask,
		"open_trades", len(liveTrades),
	)
	for _, t := range liveTrades {
		log.Debug("live runner: open position",
			"trade_id", t.ID,
			"units", t.Units,
			"entry", t.EntryPrice,
			"ticks_open", t.TicksOpen,
			"unrealized_pl", t.UnrealizedPL,
		)
	}

	// 3. Strategy decision.
	plan := cfg.Strategy.Tick(ctx, livePrice, liveTrades)
	if plan == nil {
		return nil
	}
	log.Info("live runner: plan", "reason", plan.Reason,
		"closes", len(plan.CloseIDs), "open", plan.Open != nil)

	// 4. Execute closes first.
	for _, id := range plan.CloseIDs {
		if _, err := s.OANDA.CloseTrade(ctx, s.AccountID, id, 0); err != nil {
			log.Warn("live runner: close trade failed", "trade_id", id, "err", err)
			continue
		}
		delete(tickCounts, id)
		log.Info("live runner: closed trade",
			"trade_id", id,
			"reason", plan.Reason,
		)
	}

	// 5. Execute open.
	if plan.Open == nil {
		return nil
	}
	riskPct := plan.Open.RiskPct
	if riskPct <= 0 {
		riskPct = cfg.RiskPct
	}

	log.Info("live runner: submitting order",
		"instrument", cfg.Instrument,
		"side", plan.Open.Side,
		"stop_pips", plan.Open.StopPips,
		"risk_pct", riskPct,
		"reason", plan.Open.Reason,
	)

	result, err := s.PlaceMarketOrder(ctx, PlaceMarketOrderRequest{
		Instrument:     cfg.Instrument,
		Side:           plan.Open.Side,
		RiskPct:        riskPct,
		StopPips:       plan.Open.StopPips,
		MaxUnits:       cfg.MaxUnits,
		MaxPositionUSD: cfg.MaxPositionUSD,
		Confirm:        true,
	})
	if err != nil {
		return fmt.Errorf("place order: %w", err)
	}
	if result.Filled != nil {
		log.Info("live runner: opened trade",
			"trade_id", result.Filled.TradeID,
			"instrument", cfg.Instrument,
			"side", plan.Open.Side,
			"units", result.Filled.Units,
			"price", result.Filled.Price,
			"reason", plan.Open.Reason,
		)
	}
	return nil
}

// normalizeInstrument converts "EUR/USD" → "EUR_USD" and uppercases.
func normalizeInstrument(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "/", "_"))
}

// oandaOpenTradeAdapter converts oanda.OpenTrade to the fields we need.
// Defined here to avoid a direct dependency on oanda types in the strategy layer.
type oandaOpenTradeAdapter = oanda.OpenTrade
