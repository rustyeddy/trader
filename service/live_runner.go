package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/market"
)

// LiveRunConfig controls a single live strategy run.
type LiveRunConfig struct {
	// Instrument is the OANDA instrument name, e.g. "EUR_USD".
	Instrument string

	// TickInterval is how often the runner polls for prices and ticks the
	// strategy. Defaults to 60s when zero.
	TickInterval time.Duration

	// Strategy is the live strategy to run. Required.
	Strategy LiveStrategy

	// RiskPct is the default risk per trade when the strategy's
	// LiveOpenRequest carries zero. Defaults to 0.1.
	RiskPct float64

	// MaxUnits caps position size in units (absolute). 0 = no cap.
	MaxUnits int64

	// MaxPositionUSD caps position notional value in account currency. 0 = no cap.
	MaxPositionUSD float64

	// UseStream, when true, connects to the OANDA pricing stream instead of
	// polling GetPricing on each tick. The stream runs in the background
	// keeping a latest-price cache; the timer still drives strategy evaluation
	// at TickInterval. On stream disconnect, the runner reconnects with
	// exponential backoff and falls back to GetPricing until reconnected.
	UseStream bool

	// BotID is the managed-bot identifier. When set, trades written to the
	// live journal are tagged with this ID so reports can filter by bot.
	BotID string
}

// RunLiveStrategy runs a live strategy loop until ctx is cancelled.
// On each tick it:
//  1. Fetches the current bid/ask price.
//  2. Queries open trades from the broker and increments their tick counter.
//  3. Calls strategy.Tick to get a plan.
//  4. Executes closes, then the open (if any).
func (a *Account) RunLiveStrategy(ctx context.Context, cfg LiveRunConfig) error {
	if err := validateLiveRunConfig(&cfg); err != nil {
		return err
	}
	if a.svc.OANDA == nil {
		return fmt.Errorf("live runner: OANDA client not configured")
	}

	log := a.svc.Log
	if log == nil {
		log = slog.Default()
	}

	log.Info("live runner: starting",
		"strategy", cfg.Strategy.Name(),
		"instrument", cfg.Instrument,
		"tick_interval", cfg.TickInterval,
	)

	// Start the account snapshot if it is not already running (e.g. launched
	// by the serve daemon). This seeds from the full account details and then
	// polls incremental changes, eliminating per-tick GetOpenTrades calls.
	a.EnsureSnapshot(ctx, cfg.TickInterval)

	// Start the pricing stream cache if requested. The cache is nil when
	// UseStream is false — runOneTick falls back to GetPricing in that case.
	var pxCache *priceCache
	if cfg.UseStream {
		pxCache = &priceCache{}
		go a.runPricingStream(ctx, cfg.Instrument, log, pxCache)
		log.Info("live runner: pricing stream started", "instrument", cfg.Instrument)
	}

	// tickCounts tracks how many ticks each open trade has been held.
	// Seeded from OANDA open-time on startup so a restart doesn't reset ages.
	tickCounts := a.seedTickCounts(ctx, cfg, log)

	ticker := time.NewTicker(cfg.TickInterval)
	defer ticker.Stop()

	marketWasClosed := false

	tick := func() {
		if market.IsForexMarketClosed(time.Now()) {
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
		if err := a.runOneTick(ctx, cfg, tickCounts, pxCache, log); err != nil {
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

// validateLiveRunConfig checks required fields and applies defaults in place.
func validateLiveRunConfig(cfg *LiveRunConfig) error {
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
	return nil
}

// RunLiveStrategy runs a live strategy loop on the default account. Config is
// validated (and the OANDA client checked) before the account is resolved so
// callers see precise errors without a network round-trip.
func (s *Service) RunLiveStrategy(ctx context.Context, cfg LiveRunConfig) error {
	if err := validateLiveRunConfig(&cfg); err != nil {
		return err
	}
	if s.OANDA == nil {
		return fmt.Errorf("live runner: OANDA client not configured")
	}
	acc, err := s.DefaultAccount(ctx)
	if err != nil {
		return fmt.Errorf("live runner: %w", err)
	}
	return acc.RunLiveStrategy(ctx, cfg)
}

func (a *Account) runOneTick(
	ctx context.Context,
	cfg LiveRunConfig,
	tickCounts map[string]int,
	pxCache *priceCache,
	log *slog.Logger,
) error {
	// 1. Current price — prefer stream cache when available; fall back to REST.
	var livePrice LivePrice
	if pxCache != nil {
		if tick := pxCache.get(); tick != nil {
			livePrice = LivePrice{
				Instrument: cfg.Instrument,
				Bid:        tick.Bid,
				Ask:        tick.Ask,
				Time:       tick.Time,
			}
		}
	}
	if livePrice.Bid == 0 {
		prices, err := a.svc.OANDA.GetPricing(ctx, a.ID, cfg.Instrument)
		if err != nil {
			return fmt.Errorf("get pricing: %w", err)
		}
		if len(prices) == 0 {
			return fmt.Errorf("no price for %s", cfg.Instrument)
		}
		px := prices[0]
		livePrice = LivePrice{
			Instrument: cfg.Instrument,
			Bid:        px.Bid,
			Ask:        px.Ask,
			Time:       time.Now(),
		}
	}

	// 2. Open trades on the account, filtered to this instrument.
	// Prefer the local snapshot; fall back to a direct OANDA call when the
	// snapshot is not running (e.g. unit tests without a running serve daemon).
	var allTrades []oanda.OpenTrade
	if snap := a.getSnapshot(); snap != nil {
		allTrades = snap.OpenTrades()
	} else {
		var tradesErr error
		allTrades, tradesErr = a.svc.OANDA.GetOpenTrades(ctx, a.ID)
		if tradesErr != nil {
			return fmt.Errorf("get open trades: %w", tradesErr)
		}
	}
	inst := normalizeInstrument(cfg.Instrument)
	var liveTrades []LiveTrade
	seenIDs := map[string]struct{}{}
	for _, t := range allTrades {
		if normalizeInstrument(t.Instrument) != inst {
			continue
		}
		seenIDs[t.ID] = struct{}{}
		tickCounts[t.ID]++
		liveTrades = append(liveTrades, LiveTrade{
			ID:           t.ID,
			Instrument:   t.Instrument,
			Units:        t.Units,
			EntryPrice:   t.EntryPrice,
			UnrealizedPL: t.UnrealizedPL,
			OpenTime:     t.OpenTime,
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
		"bid", livePrice.Bid, "ask", livePrice.Ask,
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
		if _, err := a.svc.OANDA.CloseTrade(ctx, a.ID, id, 0); err != nil {
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

	result, err := a.PlaceMarketOrder(ctx, PlaceMarketOrderRequest{
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
		if cfg.BotID != "" {
			a.svc.RegisterTradeBotID(result.Filled.TradeID, cfg.BotID)
		}
	}
	return nil
}

// priceCache holds the most recent tick from the OANDA pricing stream.
// A nil tick means no price has been received yet.
type priceCache struct {
	mu   sync.RWMutex
	tick *oanda.PriceTick
}

func (c *priceCache) set(t oanda.PriceTick) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tick = &t
}

func (c *priceCache) get() *oanda.PriceTick {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tick
}

// runPricingStream keeps the priceCache fresh by connecting to the OANDA
// pricing stream and reconnecting with exponential backoff on disconnect.
// It returns only when ctx is cancelled.
func (a *Account) runPricingStream(ctx context.Context, instrument string, log *slog.Logger, cache *priceCache) {
	const (
		baseDelay = 2 * time.Second
		maxDelay  = 2 * time.Minute
	)
	attempt := 0

	for {
		if ctx.Err() != nil {
			return
		}

		ch, err := a.svc.OANDA.StreamPricing(ctx, oanda.PricingStreamOptions{
			AccountID:   a.ID,
			Instruments: []string{instrument},
			OnHeartbeat: func(t time.Time) {
				log.Debug("live runner: pricing stream heartbeat", "t", t)
			},
		})
		if err != nil {
			log.Warn("live runner: pricing stream connect failed", "err", err, "attempt", attempt+1)
		} else {
			attempt = 0 // reset backoff on successful connection
			for ev := range ch {
				if ev.Err != nil {
					log.Warn("live runner: pricing stream error", "err", ev.Err)
					break
				}
				cache.set(ev.Tick)
			}
			if ctx.Err() != nil {
				return
			}
			log.Warn("live runner: pricing stream disconnected, reconnecting")
		}

		attempt++
		delay := time.Duration(math.Min(
			float64(baseDelay)*math.Pow(2, float64(attempt-1)),
			float64(maxDelay),
		))
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// seedTickCounts fetches the current open trades from OANDA at startup and
// returns a tickCounts map pre-populated with estimated ages. This ensures
// that a bot restart does not reset trade ages back to 1.
//
// The estimate is: elapsed = now − openTime, ticks = elapsed ÷ tickInterval.
// We seed with (ticks - 1) so that the first increment in runOneTick brings
// the count to the correct estimated value.
func (a *Account) seedTickCounts(ctx context.Context, cfg LiveRunConfig, log *slog.Logger) map[string]int {
	counts := map[string]int{}
	var trades []oanda.OpenTrade
	if snap := a.getSnapshot(); snap != nil {
		trades = snap.OpenTrades()
	} else {
		var err error
		trades, err = a.svc.OANDA.GetOpenTrades(ctx, a.ID)
		if err != nil {
			log.Warn("live runner: could not seed tick counts from open trades", "err", err)
			return counts
		}
	}
	inst := normalizeInstrument(cfg.Instrument)
	now := time.Now()
	for _, t := range trades {
		if normalizeInstrument(t.Instrument) != inst {
			continue
		}
		if t.OpenTime.IsZero() {
			continue
		}
		estimated := estimateTicksOpen(t.OpenTime, now, cfg.TickInterval)
		if estimated > 0 {
			counts[t.ID] = estimated - 1 // runOneTick will add 1 on first tick
		}
		log.Info("live runner: seeded tick count for existing trade",
			"trade_id", t.ID,
			"open_time", t.OpenTime,
			"estimated_ticks", estimated,
		)
	}
	return counts
}

// estimateTicksOpen returns how many tick intervals have elapsed between
// openTime and now. Returns 0 when openTime is zero or in the future.
func estimateTicksOpen(openTime, now time.Time, interval time.Duration) int {
	if openTime.IsZero() || !now.After(openTime) {
		return 0
	}
	return int(now.Sub(openTime) / interval)
}

// normalizeInstrument converts "EUR/USD" → "EUR_USD" and uppercases.
func normalizeInstrument(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "/", "_"))
}

// oandaOpenTradeAdapter converts oanda.OpenTrade to the fields we need.
// Defined here to avoid a direct dependency on oanda types in the strategy layer.
type oandaOpenTradeAdapter = oanda.OpenTrade
