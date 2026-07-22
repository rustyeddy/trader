package botsvc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/planner"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
)

// CandleStrategyAdapter wraps a backtest strategy.Strategy as a trader.LiveStrategy.
//
// On each Tick it checks whether a new completed bar has arrived since the last
// call. If yes, it feeds the bar to the underlying strategy, applies the regime
// filter, and converts the StrategyPlan to an account.LivePlan. If no new bar has closed
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
	scale           types.Scale6

	oanda           *oanda.Client
	accountID       string
	updateTradeStop func(ctx context.Context, tradeID string, stopPx, takePx float64) error // nil disables trailing stops
	log             *slog.Logger

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
	// LocalWarmupBars, when > 0, reads this many bars from the local candle
	// store (set via datamanager.SetDataDir / --data-dir) before the OANDA warmup
	// fetch. Use 500+ to ensure long-period regime filters and ATR percentile
	// indicators are fully primed. Falls back gracefully if local data is absent.
	WarmupBars      int // bars to fetch from OANDA for indicator warmup (default 100)
	LocalWarmupBars int
	OANDA           *oanda.Client
	AccountID       string
	// UpdateTradeStop, if non-nil, is called to push a moved trailing stop to
	// the broker. nil disables trailing stops.
	UpdateTradeStop func(ctx context.Context, tradeID string, stopPx, takePx float64) error
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
		scale:           types.PriceScale,
		oanda:           cfg.OANDA,
		accountID:       cfg.AccountID,
		updateTradeStop: cfg.UpdateTradeStop,
		log:             log,
	}
}

func (a *CandleStrategyAdapter) Name() string {
	return fmt.Sprintf("%s/%s/%s", a.strategy.Name(), a.instNorm, a.granularity)
}

// Tick implements trader.LiveStrategy. It is called by the live runner on every
// price poll tick regardless of bar frequency.
func (a *CandleStrategyAdapter) Tick(ctx context.Context, price account.LivePrice, openTrades []account.LiveTrade) *account.LivePlan {
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
	a.exit.Tick(ct)

	// Update trailing stops on open positions and push to OANDA if they moved.
	if a.exit.Ready() {
		a.updateTrailingStops(ctx, ct)
	}

	// Build a synthetic backtest object so the strategy can inspect open lots.
	bt := a.makeBacktest()

	sig := a.strategy.Update(ctx, &ct, bt)

	pc := livePlanContext{instrument: a.instNorm, exit: a.exit, regime: a.regime, candle: ct}
	plan, _, err := planner.DefaultPlanner{}.PlanSignal(sig, pc)
	if err != nil {
		a.log.Error("candle adapter: PlanSignal error", "err", err, "instrument", a.instNorm)
		return nil
	}

	// In live mode, the planner has no account and cannot populate closes from
	// account lots. When the strategy signals CloseAll, populate closes directly
	// from the live lot tracker so the runner can close the corresponding trades.
	if sig.CloseAll {
		for _, lot := range a.lots.byID {
			plan.Closes = append(plan.Closes, &account.CloseRequest{
				Request: account.Request{
					TradeCommon: lot.TradeCommon,
					Reason:      sig.Reason,
				},
				Lot:        lot,
				CloseCause: account.CloseManual,
			})
		}
	}

	if plan.Empty() {
		return nil
	}

	if len(plan.Opens) > 0 {
		a.log.Info("live: strategy signal open",
			"instrument", a.instNorm,
			"side", sig.Side,
			"reason", sig.Reason,
			"bar_time", bar.Time,
		)
	}
	if len(plan.Closes) > 0 {
		a.log.Info("live: strategy signal close",
			"instrument", a.instNorm,
			"count", len(plan.Closes),
			"reason", sig.Reason,
			"bar_time", bar.Time,
		)
	}

	return a.convertPlan(plan, price)
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

	dm := datamanager.NewDataManager([]string{a.instNorm}, from, to)
	iter, err := dm.Candles(ctx, datamanager.CandleRequest{
		Source:     market.SourceOanda,
		Instrument: a.instNorm,
		Range:      types.TimeRange{Start: types.FromTime(from), End: types.FromTime(to), TF: tf},
	})
	if err != nil {
		return fmt.Errorf("load local candles: %w", err)
	}
	defer func() { _ = iter.Close() }()

	count := 0
	for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
		a.regime.Tick(ct)
		a.exit.Tick(ct)
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
		a.exit.Tick(ct)
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

// convertPlan converts a finalized StrategyPlan (produced by PlanSignal) to an account.LivePlan.
// Stop and regime filtering have already been applied; this method only translates
// types and computes the stop-pips distance for the OANDA wire format.
func (a *CandleStrategyAdapter) convertPlan(plan *strategy.StrategyPlan, _ account.LivePrice) *account.LivePlan {
	if plan == nil {
		return nil
	}

	lp := &account.LivePlan{Reason: plan.Reason}

	// Collect close IDs — map from internal lot ID (which we set to OANDA trade ID).
	for _, cl := range plan.Closes {
		if cl.Lot != nil {
			lp.CloseIDs = append(lp.CloseIDs, cl.Lot.ID)
		}
	}

	// Convert first open request, if any.
	if len(plan.Opens) > 0 {
		req := plan.Opens[0]

		if req.Stop == 0 {
			a.log.Error("candle adapter: open has no stop after PlanSignal — skipping open",
				"instrument", a.instNorm, "side", req.Side, "reason", plan.Reason)
		} else {
			inst := market.GetInstrument(a.instNorm)
			if inst == nil {
				a.log.Error("candle adapter: unknown instrument — skipping open", "instrument", a.instNorm)
			} else {
				entryPrice := req.Price
				dist := entryPrice - req.Stop
				if dist < 0 {
					dist = -dist
				}
				var stopPips types.Pips
				if perPip := inst.PriceUnitsPerPip(); perPip > 0 {
					// Rounding integer division: (dist×10 + perPip/2) / perPip.
					stopPips = types.Pips((int64(dist)*10 + int64(perPip)/2) / int64(perPip))
				}

				side := "long"
				if req.Side == types.Short {
					side = "short"
				}
				a.log.Info("live: open order queued",
					"instrument", a.instNorm,
					"side", side,
					"entry_price", entryPrice.Float64(),
					"stop_price", req.Stop.Float64(),
					"stop_pips", stopPips.Float64(),
					"reason", plan.Reason,
				)
				lp.Open = &account.LiveOpenRequest{
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

// oandaCandleToCandleTime converts an OANDA candle to the internal Candle
// type used by backtest strategies. Uses the mid (bid+ask)/2 for each OHLC.
func oandaCandleToCandleTime(c oanda.Candle, _ string) market.Candle {
	toPrice := func(bid, ask float64) types.Price {
		return types.PriceFromFloat((bid + ask) / 2)
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
	candle.Timestamp = types.FromTime(c.Time)
	return candle
}

// oandaGranToTF converts an OANDA granularity string ("H1", "D", "M1") to the
// internal types.Timeframe constant used by the candle store.
func oandaGranToTF(granularity string) types.Timeframe {
	switch strings.ToUpper(strings.TrimSpace(granularity)) {
	case "D", "D1":
		return types.D1
	case "M1":
		return types.M1
	default:
		return types.H1
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
func (a *CandleStrategyAdapter) updateTrailingStops(ctx context.Context, ct market.Candle) {
	if a.updateTradeStop == nil {
		return // trailing stops disabled
	}
	for id, meta := range a.lots.meta {
		lot := a.lots.byID[id]
		if lot == nil {
			continue
		}
		// Advance extreme price watermark.
		switch lot.Side {
		case types.Long:
			if meta.extremePrice == 0 || ct.High > meta.extremePrice {
				meta.extremePrice = ct.High
			}
		case types.Short:
			if meta.extremePrice == 0 || ct.Low < meta.extremePrice {
				meta.extremePrice = ct.Low
			}
		}
		newStop := a.exit.UpdateStop(lot.Side, meta.currentStop, lot.EntryPrice, meta.extremePrice, ct)
		if newStop == meta.currentStop || newStop == 0 {
			continue
		}
		// Stop moved — push to OANDA.
		stopFloat := newStop.Float64()
		if err := a.updateTradeStop(ctx, id, stopFloat, 0); err != nil {
			a.log.Warn("candle adapter: trailing stop update failed",
				"trade_id", id, "stop", stopFloat, "err", err)
			continue
		}
		a.log.Info("candle adapter: trailing stop updated",
			"trade_id", id, "instrument", a.instrument,
			"old_stop", meta.currentStop.Float64(),
			"new_stop", stopFloat,
		)
		meta.currentStop = newStop
	}
}

// ── live PlanContext ─────────────────────────────────────────────────────────

// livePlanContext implements planner.PlanContext for the live trading path.
// Account returns nil so PlanSignal skips sizing (OANDA handles risk/sizing).
// MaxSpread and Slippage are zero — live fills are at market; the spread gate
// and slippage adjustment are backtest-only execution-cost models.
type livePlanContext struct {
	instrument string
	exit       strategy.ExitStrategy
	regime     strategy.RegimeFilter
	candle     market.Candle
}

func (c livePlanContext) Instrument() string            { return c.instrument }
func (c livePlanContext) Account() *account.Account     { return nil }
func (c livePlanContext) Exit() strategy.ExitStrategy   { return c.exit }
func (c livePlanContext) Regime() strategy.RegimeFilter { return c.regime }
func (c livePlanContext) Candle() market.Candle         { return c.candle }
func (c livePlanContext) Slippage() types.Price         { return 0 }
func (c livePlanContext) MaxSpread() types.Price        { return 0 }
func (c livePlanContext) DefaultStopPips() types.Pips   { return 0 }

// ── lot tracker ──────────────────────────────────────────────────────────────

// lotMeta carries the state the adapter needs beyond what OANDA provides.
type lotMeta struct {
	currentStop  types.Price // last stop we've set (in scaled Price units)
	extremePrice types.Price // highest high (long) or lowest low (short) seen since entry
}

// liveLotsTracker maintains a shadow lot book that mirrors OANDA open trades.
// Lot IDs are set to the OANDA trade ID so close requests can be routed back.
type liveLotsTracker struct {
	byID map[string]*account.Lot // key = OANDA trade ID
	meta map[string]*lotMeta
}

func (lt *liveLotsTracker) sync(trades []account.LiveTrade) {
	seen := map[string]struct{}{}
	for _, t := range trades {
		seen[t.ID] = struct{}{}
		if lt.byID == nil {
			lt.byID = map[string]*account.Lot{}
			lt.meta = map[string]*lotMeta{}
		}
		if _, ok := lt.byID[t.ID]; !ok {
			side := types.Long
			if t.Units < 0 {
				side = types.Short
			}
			tc := &account.TradeCommon{ID: t.ID}
			tc.Side = side
			entryPrice := t.EntryPrice
			tc.Stop = entryPrice // placeholder; real stop set by adapter
			lt.byID[t.ID] = &account.Lot{
				TradeCommon: tc,
				EntryPrice:  entryPrice,
				State:       account.LotOpen,
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

func (lt *liveLotsTracker) toLotBook() *account.LotBook {
	lb := &account.LotBook{}
	for _, lot := range lt.byID {
		_ = lb.Add(lot.Clone())
	}
	return lb
}
