package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/brokers/oanda"
)

// CandleStrategyAdapter wraps a backtest trader.Strategy as a trader.LiveStrategy.
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
	strategy    trader.Strategy
	regime      trader.RegimeFilter
	instrument  string // OANDA format, e.g. "EUR_USD"
	instNorm    string // internal format, e.g. "EURUSD"
	granularity string // OANDA granularity, e.g. "H1", "D"
	warmupBars  int
	scale       trader.Scale6

	oanda     *oanda.Client
	accountID string
	log       *slog.Logger

	lastBarTime time.Time
	warmedUp    bool
	lots        liveLotsTracker
}

// CandleAdapterConfig configures a CandleStrategyAdapter.
type CandleAdapterConfig struct {
	Strategy    trader.Strategy
	Regime      trader.RegimeFilter // nil means NoopRegime
	Instrument  string              // OANDA format
	Granularity string              // "H1" or "D"
	WarmupBars  int                 // bars to fetch for indicator warmup (default 100)
	OANDA       *oanda.Client
	AccountID   string
	Log         *slog.Logger
}

// NewCandleStrategyAdapter constructs a ready-to-use adapter.
func NewCandleStrategyAdapter(cfg CandleAdapterConfig) *CandleStrategyAdapter {
	regime := cfg.Regime
	if regime == nil {
		regime = trader.NoopRegime{}
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
		strategy:    cfg.Strategy,
		regime:      regime,
		instrument:  cfg.Instrument,
		instNorm:    trader.NormalizeInstrument(cfg.Instrument),
		granularity: cfg.Granularity,
		warmupBars:  warmup,
		scale:       trader.PriceScale,
		oanda:       cfg.OANDA,
		accountID:   cfg.AccountID,
		log:         log,
	}
}

func (a *CandleStrategyAdapter) Name() string {
	return fmt.Sprintf("%s/%s/%s", a.strategy.Name(), a.instNorm, a.granularity)
}

// Tick implements trader.LiveStrategy. It is called by the live runner on every
// price poll tick regardless of bar frequency.
func (a *CandleStrategyAdapter) Tick(ctx context.Context, price trader.LivePrice, openTrades []trader.LiveTrade) *trader.LivePlan {
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

	// Advance regime filter.
	a.regime.Tick(ct)

	// Build a synthetic backtest object so the strategy can inspect open lots.
	bt := a.makeBacktest()

	plan := a.strategy.Update(ctx, &ct, bt)
	if plan == nil {
		return nil
	}

	// Apply regime filter to opens (mirrors the backtest loop in trader.go).
	if a.regime.Ready() && len(plan.Opens) > 0 {
		if !a.regime.Trending() {
			plan.Opens = nil
		} else {
			filtered := plan.Opens[:0]
			for _, o := range plan.Opens {
				if a.regime.AllowSide(o.Side) {
					filtered = append(filtered, o)
				}
			}
			plan.Opens = filtered
		}
	}

	return a.convertPlan(plan, ct, price)
}

// warmup fetches historical bars and replays them through the strategy and
// regime filter without emitting signals.
func (a *CandleStrategyAdapter) warmup(ctx context.Context) error {
	to := time.Now().UTC()
	// Fetch enough bars to cover indicator warmup periods plus some buffer.
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

	a.log.Info("candle adapter: warming up",
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
		// Call Update but discard the plan — warmup only.
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
func (a *CandleStrategyAdapter) makeBacktest() *trader.Backtest {
	lb := a.lots.toLotBook()
	return &trader.Backtest{
		BacktestRequest: &trader.BacktestRequest{
			Instrument: a.instNorm,
		},
		BacktestRun: &trader.BacktestRun{
			Lots: lb,
		},
	}
}

// convertPlan converts a backtest StrategyPlan to a LivePlan.
func (a *CandleStrategyAdapter) convertPlan(plan *trader.StrategyPlan, ct trader.CandleTime, _ trader.LivePrice) *trader.LivePlan {
	if plan == nil {
		return nil
	}

	live := &trader.LivePlan{Reason: plan.Reason}

	// Collect close IDs — map from internal lot ID (which we set to OANDA trade ID).
	for _, cl := range plan.Closes {
		if cl.Lot != nil {
			live.CloseIDs = append(live.CloseIDs, cl.Lot.ID)
		}
	}

	// Convert first open request, if any.
	if len(plan.Opens) > 0 {
		req := plan.Opens[0]
		inst := trader.GetInstrument(a.instNorm)
		stopPips := 0.0
		if inst != nil && req.Stop != 0 {
			entryPrice := ct.Close
			dist := entryPrice - req.Stop
			if dist < 0 {
				dist = -dist
			}
			perPip := inst.PriceUnitsPerPip()
			if perPip > 0 {
				stopPips = math.Round(float64(dist)/float64(perPip)*10) / 10
			}
		}

		side := "long"
		if req.Side == trader.Short {
			side = "short"
		}
		live.Open = &trader.LiveOpenRequest{
			Side:     side,
			StopPips: stopPips,
		}
	}

	if live.Open == nil && len(live.CloseIDs) == 0 {
		return nil
	}
	return live
}

// oandaCandleToCandleTime converts an OANDA candle to the internal CandleTime
// type used by backtest strategies. Uses the mid (bid+ask)/2 for each OHLC.
func oandaCandleToCandleTime(c oanda.Candle, _ string) trader.CandleTime {
	scale := float64(trader.PriceScale)
	toPrice := func(bid, ask float64) trader.Price {
		return trader.Price(math.Round(((bid + ask) / 2) * scale))
	}
	candle := trader.Candle{
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
	return trader.CandleTime{
		Candle:    candle,
		Timestamp: trader.FromTime(c.Time),
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

// ── lot tracker ──────────────────────────────────────────────────────────────

// liveLotsTracker maintains a shadow lot book that mirrors OANDA open trades.
// Lot IDs are set to the OANDA trade ID so close requests can be routed back.
type liveLotsTracker struct {
	byID map[string]*trader.Lot // key = OANDA trade ID
}

func (lt *liveLotsTracker) sync(trades []trader.LiveTrade) {
	seen := map[string]struct{}{}
	for _, t := range trades {
		seen[t.ID] = struct{}{}
		if lt.byID == nil {
			lt.byID = map[string]*trader.Lot{}
		}
		if _, ok := lt.byID[t.ID]; !ok {
			side := trader.Long
			if t.Units < 0 {
				side = trader.Short
			}
			tc := &trader.TradeCommon{ID: t.ID}
			tc.Side = side
			lt.byID[t.ID] = &trader.Lot{
				TradeCommon: tc,
				State:       trader.LotOpen,
			}
		}
	}
	// Remove lots that have closed on the broker side.
	for id := range lt.byID {
		if _, ok := seen[id]; !ok {
			delete(lt.byID, id)
		}
	}
}

func (lt *liveLotsTracker) toLotBook() *trader.LotBook {
	lb := &trader.LotBook{}
	for _, lot := range lt.byID {
		lb.Add(lot)
	}
	return lb
}
