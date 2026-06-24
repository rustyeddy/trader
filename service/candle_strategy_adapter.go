package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/live"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/strategy"
)

// CandleStrategyAdapter wraps a backtest strategy.Strategy as a trader.LiveStrategy.
//
// On each Tick it checks whether a new completed bar has arrived since the last
// call. If yes, it feeds the bar to the underlying strategy, applies the regime
// filter, and converts the StrategyPlan to a LivePlan. If no new bar has closed
// it returns nil (hold).
//
// On first use the adapter fetches the last WarmupBars completed candles from
// OANDA and replays them silently so all indicators are primed before the first
// live signal is emitted.
type CandleStrategyAdapter struct {
	strategy        strategy.Strategy
	exit            strategy.ExitStrategy
	regime          strategy.RegimeFilter
	instrument      string // OANDA format, e.g. "EUR_USD"
	instNorm        string // internal format, e.g. "EURUSD"
	granularity     string // OANDA granularity, e.g. "H1", "D"
	warmupBars      int
	localWarmupBars int
	scale           market.Scale6

	oanda     *oanda.Client
	accountID string
	svc       *Service // for UpdateTradeStop calls
	log       *slog.Logger

	lastBarTime time.Time
	warmedUp    bool
	lots        liveLotsTracker
}

// CandleAdapterConfig configures a CandleStrategyAdapter.
type CandleAdapterConfig struct {
	Strategy    strategy.Strategy
	Exit        strategy.ExitStrategy // nil means NoopExit (no trailing stop)
	Regime      strategy.RegimeFilter // nil means NoopRegime
	Instrument  string                // OANDA format
	Granularity string                // "H1" or "D"
	WarmupBars  int                   // bars to fetch from OANDA for indicator warmup (default 100)
	// LocalWarmupBars, when > 0, reads this many bars from the local candle
	// store (set via marketdata.SetDataDir / --data-dir) before the OANDA warmup
	// fetch. Use 500+ to ensure long-period regime filters and ATR percentile
	// indicators are fully primed. Falls back gracefully if local data is absent.
	LocalWarmupBars int
	OANDA           *oanda.Client
	AccountID       string
	Service         *Service // required for UpdateTradeStop; nil disables trailing stops
	Log             *slog.Logger
}

// NewCandleStrategyAdapter constructs a ready-to-use adapter.
func NewCandleStrategyAdapter(cfg CandleAdapterConfig) *CandleStrategyAdapter {
	regime := cfg.Regime
	if regime == nil {
		regime = strategy.NoopRegime{}
	}
	exit := cfg.Exit
	if exit == nil {
		exit = strategy.NoopExit{}
	}
	warmup := cfg.WarmupBars
	if warmup <= 0 {
		warmup = 100
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &CandleStrategyAdapter{
		strategy:        cfg.Strategy,
		exit:            exit,
		regime:          regime,
		instrument:      cfg.Instrument,
		instNorm:        market.NormalizeInstrument(cfg.Instrument),
		granularity:     cfg.Granularity,
		warmupBars:      warmup,
		localWarmupBars: cfg.LocalWarmupBars,
		scale:           market.PriceScale,
		oanda:           cfg.OANDA,
		accountID:       cfg.AccountID,
		svc:             cfg.Service,
		log:             log,
	}
}

func (a *CandleStrategyAdapter) Name() string {
	return fmt.Sprintf("%s/%s/%s", a.strategy.Name(), a.instNorm, a.granularity)
}

// Tick implements trader.LiveStrategy. It is called by the live runner on every
// price poll tick regardless of bar frequency.
func (a *CandleStrategyAdapter) Tick(ctx context.Context, price live.LivePrice, openTrades []live.LiveTrade) *live.LivePlan {
	// Sync our lot tracker with the current live positions.
	a.lots.sync(openTrades)

	if !a.warmedUp {
		if err := a.warmup(ctx); err != nil {
			a.log.Warn("candle adapter: warmup failed", "err", err, "instrument", a.instrument)
			return nil
		}
		a.warmedUp = true
	}

	// Fetch the most recent complete bar.
	bar, err := a.latestCompleteBar(ctx)
	if err != nil {
		a.log.Warn("candle adapter: fetch bar failed", "err", err)
		return nil
	}
	if bar == nil || !bar.Time.After(a.lastBarTime) {
		return nil // no new bar yet
	}
	a.lastBarTime = bar.Time

	ct := oandaCandleToCandleTime(*bar, a.instNorm)

	// Advance regime filter and exit strategy indicators.
	a.regime.Tick(ct)
	a.exit.Tick(ct.Candle)

	// Update trailing stops on open positions and push to OANDA if they moved.
	if a.exit.Ready() {
		a.updateTrailingStops(ctx, ct)
	}

	// Build a synthetic backtest object so the strategy can inspect open lots.
	bt := a.makeBacktest()

	plan := a.strategy.Update(ctx, &ct, bt)
	if plan == nil {
		return nil
	}

	if len(plan.Opens) > 0 {
		a.log.Info("live: strategy signal open",
			"instrument", a.instNorm,
			"side", plan.Opens[0].Side,
			"stop", plan.Opens[0].Stop,
			"reason", plan.Reason,
			"bar_time", bar.Time,
		)
	}
	if len(plan.Closes) > 0 {
		a.log.Info("live: strategy signal close",
			"instrument", a.instNorm,
			"count", len(plan.Closes),
			"reason", plan.Reason,
			"bar_time", bar.Time,
		)
	}

	// Apply regime filter to opens (mirrors the backtest loop in trader.go).
	if a.regime.Ready() && len(plan.Opens) > 0 {
		if !a.regime.Trending() {
			a.log.Info("live: open blocked by regime filter — not trending",
				"instrument", a.instNorm,
				"bar_time", bar.Time,
			)
			plan.Opens = nil
		} else {
			filtered := plan.Opens[:0]
			for _, o := range plan.Opens {
				if a.regime.AllowSide(o.Side) {
					filtered = append(filtered, o)
				} else {
					a.log.Info("live: open blocked by regime filter — side not allowed",
						"instrument", a.instNorm,
						"side", o.Side,
						"bar_time", bar.Time,
					)
				}
			}
			plan.Opens = filtered
		}
	}

	return a.convertPlan(plan, ct, price)
}

// warmup primes all indicators before the first live signal is emitted.
// When localWarmupBars > 0 it first replays bars from the local candle store
// (which may span years), then tops up with a short OANDA fetch to cover any
// gap between the newest local bar and now.
func (a *CandleStrategyAdapter) warmup(ctx context.Context) error {
	if a.localWarmupBars > 0 {
		if err := a.warmupFromLocalData(ctx); err != nil {
			a.log.Warn("candle adapter: local warmup failed, continuing with OANDA-only warmup",
				"err", err, "instrument", a.instrument)
		}
	}
	return a.warmupFromOANDA(ctx)
}

// warmupFromLocalData reads up to localWarmupBars bars from the local candle
// store and replays them through the strategy, regime filter, and exit strategy.
// Missing months are silently skipped so the adapter starts up even when data
// is incomplete.
func (a *CandleStrategyAdapter) warmupFromLocalData(ctx context.Context) error {
	to := time.Now().UTC()
	from := barsBefore(to, a.granularity, a.localWarmupBars)
	tf := oandaGranToTF(a.granularity)

	dm := marketdata.NewDataManager([]string{a.instNorm}, from, to)
	iter, err := dm.Candles(ctx, marketdata.CandleRequest{
		Source:     market.SourceOanda,
		Instrument: a.instNorm,
		Range:      market.TimeRange{Start: market.FromTime(from), End: market.FromTime(to), TF: tf},
	})
	if err != nil {
		return fmt.Errorf("load local candles: %w", err)
	}
	defer func() { _ = iter.Close() }()

	count := 0
	for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
		a.regime.Tick(ct)
		a.exit.Tick(ct.Candle)
		bt := a.makeBacktest()
		_ = a.strategy.Update(ctx, &ct, bt)
		if barTime := ct.Timestamp.Time(); barTime.After(a.lastBarTime) {
			a.lastBarTime = barTime
		}
		count++
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("iterate local candles: %w", err)
	}

	a.log.Info("candle adapter: local warmup complete",
		"instrument", a.instrument,
		"bars", count,
		"from", from.Format("2006-01-02"),
		"last_bar", a.lastBarTime.Format("2006-01-02"),
		"exit_ready", a.exit.Ready(),
	)
	return nil
}

// warmupFromOANDA fetches recent bars from OANDA to cover any gap between the
// newest local bar and now, and to ensure all indicators see the latest prices.
func (a *CandleStrategyAdapter) warmupFromOANDA(ctx context.Context) error {
	to := time.Now().UTC()
	from := barsBefore(to, a.granularity, a.warmupBars+10)

	candles, err := a.oanda.FetchCandles(ctx, oanda.FetchCandlesOptions{
		Instrument:  a.instrument,
		Granularity: a.granularity,
		From:        from,
		To:          to,
	})
	if err != nil {
		return fmt.Errorf("fetch warmup candles: %w", err)
	}

	a.log.Info("candle adapter: OANDA warmup",
		"instrument", a.instrument,
		"bars", len(candles),
		"strategy", a.strategy.Name(),
	)

	for _, c := range candles {
		if !c.Complete {
			continue
		}
		ct := oandaCandleToCandleTime(c, a.instNorm)
		a.regime.Tick(ct)
		a.exit.Tick(ct.Candle)
		bt := a.makeBacktest()
		_ = a.strategy.Update(ctx, &ct, bt)
		a.lastBarTime = c.Time
	}
	return nil
}

// latestCompleteBar returns the most recently completed candle, or nil if the
// current bar is still forming.
func (a *CandleStrategyAdapter) latestCompleteBar(ctx context.Context) (*oanda.Candle, error) {
	to := time.Now().UTC()
	from := barsBefore(to, a.granularity, 3)

	candles, err := a.oanda.FetchCandles(ctx, oanda.FetchCandlesOptions{
		Instrument:  a.instrument,
		Granularity: a.granularity,
		From:        from,
		To:          to,
	})
	if err != nil {
		return nil, err
	}

	// Walk backwards to find the last complete bar.
	for i := len(candles) - 1; i >= 0; i-- {
		if candles[i].Complete {
			return &candles[i], nil
		}
	}
	return nil, nil
}

// makeBacktest builds a minimal *trader.Backtest so the strategy can inspect
// the current open lots.
func (a *CandleStrategyAdapter) makeBacktest() *backtest.Backtest {
	lb := a.lots.toLotBook()
	return &backtest.Backtest{
		Request: &backtest.BacktestRequest{
			Instrument: a.instNorm,
		},
		State: &backtest.BacktestRun{
			Lots: lb,
		},
	}
}

// convertPlan converts a backtest StrategyPlan to a LivePlan.
func (a *CandleStrategyAdapter) convertPlan(plan *strategy.StrategyPlan, ct market.CandleTime, _ live.LivePrice) *live.LivePlan {
	if plan == nil {
		return nil
	}

	lp := &live.LivePlan{Reason: plan.Reason}

	// Collect close IDs — map from internal lot ID (which we set to OANDA trade ID).
	for _, cl := range plan.Closes {
		if cl.Lot != nil {
			lp.CloseIDs = append(lp.CloseIDs, cl.Lot.ID)
		}
	}

	// Convert first open request, if any.
	if len(plan.Opens) > 0 {
		req := plan.Opens[0]

		// Mirror the backtest loop (trader.go): if the strategy did not set a
		// stop, ask the exit strategy for the initial stop price.
		if req.Stop == 0 && a.exit.Ready() {
			req.Stop = a.exit.InitialStop(req.Side, ct.Close, ct.Candle)
		}

		if req.Stop == 0 {
			a.log.Error("candle adapter: strategy returned open with no stop — exit strategy also provided none; skipping open",
				"instrument", a.instNorm, "side", req.Side, "reason", plan.Reason)
		} else {
			inst := market.GetInstrument(a.instNorm)
			if inst == nil {
				a.log.Error("candle adapter: unknown instrument — skipping open", "instrument", a.instNorm)
			} else {
				entryPrice := ct.Close
				dist := entryPrice - req.Stop
				if dist < 0 {
					dist = -dist
				}
				stopPips := 0.0
				if perPip := inst.PriceUnitsPerPip(); perPip > 0 {
					stopPips = math.Round(float64(dist)/float64(perPip)*10) / 10
				}

				side := "long"
				if req.Side == market.Short {
					side = "short"
				}
				scale := float64(a.scale)
				a.log.Info("live: open order queued",
					"instrument", a.instNorm,
					"side", side,
					"entry_price", float64(entryPrice)/scale,
					"stop_price", float64(req.Stop)/scale,
					"stop_pips", stopPips,
					"reason", plan.Reason,
				)
				lp.Open = &live.LiveOpenRequest{
					Side:     side,
					StopPips: stopPips,
					Reason:   plan.Reason,
				}
			}
		}
	}

	if lp.Open == nil && len(lp.CloseIDs) == 0 {
		return nil
	}
	return lp
}

// oandaCandleToCandleTime converts an OANDA candle to the internal CandleTime
// type used by backtest strategies. Uses the mid (bid+ask)/2 for each OHLC.
func oandaCandleToCandleTime(c oanda.Candle, _ string) market.CandleTime {
	scale := float64(market.PriceScale)
	toPrice := func(bid, ask float64) market.Price {
		return market.Price(math.Round(((bid + ask) / 2) * scale))
	}
	candle := market.Candle{
		Open:  toPrice(c.BidOpen, c.AskOpen),
		High:  toPrice(c.BidHigh, c.AskHigh),
		Low:   toPrice(c.BidLow, c.AskLow),
		Close: toPrice(c.BidClose, c.AskClose),
	}
	spread := toPrice(0, c.AskClose) - toPrice(c.BidClose, 0)
	if spread < 0 {
		spread = -spread
	}
	candle.AvgSpread = spread
	return market.CandleTime{
		Candle:    candle,
		Timestamp: market.FromTime(c.Time),
	}
}

// oandaGranToTF converts an OANDA granularity string ("H1", "D", "M1") to the
// internal market.Timeframe constant used by the candle store.
func oandaGranToTF(granularity string) market.Timeframe {
	switch strings.ToUpper(strings.TrimSpace(granularity)) {
	case "D", "D1":
		return market.D1
	case "M1":
		return market.M1
	default:
		return market.H1
	}
}

// barsBefore returns a time that is approximately n bars before t for the
// given granularity. Used for warmup and recent-bar fetches.
func barsBefore(t time.Time, granularity string, n int) time.Time {
	var dur time.Duration
	switch granularity {
	case "D":
		dur = time.Duration(n) * 24 * time.Hour
	case "H1":
		dur = time.Duration(n) * time.Hour
	case "H4":
		dur = time.Duration(n) * 4 * time.Hour
	default:
		dur = time.Duration(n) * time.Minute
	}
	// Add 20% buffer for weekends/holidays.
	return t.Add(-time.Duration(float64(dur) * 1.4))
}

// updateTrailingStops iterates open lots, computes the new chandelier stop,
// and calls UpdateTradeStop on OANDA if the stop has moved.
func (a *CandleStrategyAdapter) updateTrailingStops(ctx context.Context, ct market.CandleTime) {
	if a.svc == nil {
		return // no service — trailing stops disabled
	}
	scale := float64(a.scale)
	for id, meta := range a.lots.meta {
		lot := a.lots.byID[id]
		if lot == nil {
			continue
		}
		// Advance extreme price watermark.
		switch lot.Side {
		case market.Long:
			if meta.extremePrice == 0 || ct.High > meta.extremePrice {
				meta.extremePrice = ct.High
			}
		case market.Short:
			if meta.extremePrice == 0 || ct.Low < meta.extremePrice {
				meta.extremePrice = ct.Low
			}
		}
		newStop := a.exit.UpdateStop(lot.Side, meta.currentStop, lot.EntryPrice, meta.extremePrice, ct.Candle)
		if newStop == meta.currentStop || newStop == 0 {
			continue
		}
		// Stop moved — push to OANDA.
		stopFloat := float64(newStop) / scale
		if err := a.svc.UpdateTradeStop(ctx, id, stopFloat, 0); err != nil {
			a.log.Warn("candle adapter: trailing stop update failed",
				"trade_id", id, "stop", stopFloat, "err", err)
			continue
		}
		a.log.Info("candle adapter: trailing stop updated",
			"trade_id", id, "instrument", a.instrument,
			"old_stop", float64(meta.currentStop)/scale,
			"new_stop", stopFloat,
		)
		meta.currentStop = newStop
	}
}

// ── lot tracker ──────────────────────────────────────────────────────────────

// lotMeta carries the state the adapter needs beyond what OANDA provides.
type lotMeta struct {
	currentStop  market.Price // last stop we've set (in scaled Price units)
	extremePrice market.Price // highest high (long) or lowest low (short) seen since entry
}

// liveLotsTracker maintains a shadow lot book that mirrors OANDA open trades.
// Lot IDs are set to the OANDA trade ID so close requests can be routed back.
type liveLotsTracker struct {
	byID map[string]*execution.Lot // key = OANDA trade ID
	meta map[string]*lotMeta
}

func (lt *liveLotsTracker) sync(trades []live.LiveTrade) {
	seen := map[string]struct{}{}
	for _, t := range trades {
		seen[t.ID] = struct{}{}
		if lt.byID == nil {
			lt.byID = map[string]*execution.Lot{}
			lt.meta = map[string]*lotMeta{}
		}
		if _, ok := lt.byID[t.ID]; !ok {
			side := market.Long
			if t.Units < 0 {
				side = market.Short
			}
			tc := &execution.TradeCommon{ID: t.ID}
			tc.Side = side
			scale := float64(market.PriceScale)
			entryPrice := market.Price(math.Round(t.EntryPrice * scale))
			tc.Stop = entryPrice // placeholder; real stop set by adapter
			lt.byID[t.ID] = &execution.Lot{
				TradeCommon: tc,
				EntryPrice:  entryPrice,
				State:       execution.LotOpen,
			}
			lt.meta[t.ID] = &lotMeta{}
		}
	}
	// Remove lots that have closed on the broker side.
	for id := range lt.byID {
		if _, ok := seen[id]; !ok {
			delete(lt.byID, id)
			delete(lt.meta, id)
		}
	}
}

// setInitialStop records the initial stop price for a newly opened trade.
func (lt *liveLotsTracker) setInitialStop(tradeID string, stop market.Price) {
	if lt.meta == nil {
		return
	}
	if m, ok := lt.meta[tradeID]; ok {
		m.currentStop = stop
	}
}

func (lt *liveLotsTracker) toLotBook() *execution.LotBook {
	lb := &execution.LotBook{}
	for _, lot := range lt.byID {
		_ = lb.Add(lot.Clone())
	}
	return lb
}
