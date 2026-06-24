package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/strategy"
)

// ── Signal types ─────────────────────────────────────────────────────────────

// SignalKind describes what happened at a given bar during replay.
type SignalKind string

const (
	SignalOpen       SignalKind = "open"        // strategy signalled an entry
	SignalClose      SignalKind = "close"       // strategy signalled an exit
	SignalBlocked    SignalKind = "blocked"     // regime filter suppressed an open
	SignalNoStop     SignalKind = "no_stop"     // open dropped: no stop available
	SignalStopUpdate SignalKind = "stop_update" // trailing stop ratcheted
)

// Signal records a single strategy decision or indicator event.
type Signal struct {
	Time      int64      `json:"time"` // bar open time, unix seconds
	Kind      SignalKind `json:"kind"`
	Side      string     `json:"side,omitempty"`       // "long" or "short"
	Price     float64    `json:"price,omitempty"`      // entry price (open) or exit price (close)
	StopPrice float64    `json:"stop_price,omitempty"` // stop at time of signal
	StopPips  float64    `json:"stop_pips,omitempty"`  // stop distance in pips
	Reason    string     `json:"reason,omitempty"`     // strategy reason string
}

// ReplayCandleBar is the lightweight-charts JSON shape: unix-second time + OHLC.
type ReplayCandleBar struct {
	Time  int64   `json:"time"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}

// ReplayResult is returned by RunReplay. It contains the full bar series and
// every signal emitted by the strategy over the replay window.
type ReplayResult struct {
	Instrument string            `json:"instrument"`
	Timeframe  string            `json:"timeframe"`
	Strategy   string            `json:"strategy"`
	From       string            `json:"from"`
	To         string            `json:"to"`
	WarmupBars int               `json:"warmup_bars"`
	Bars       []ReplayCandleBar `json:"bars"`
	Signals    []Signal          `json:"signals"`
}

// ── ReplayRequest ────────────────────────────────────────────────────────────

// ReplayRequest drives a single-instrument strategy replay against stored candles.
type ReplayRequest struct {
	Instrument string                  `json:"instrument"`  // internal format, e.g. "EURUSD"
	Timeframe  string                  `json:"timeframe"`   // "H1" or "D"
	From       string                  `json:"from"`        // "YYYY-MM-DD"
	To         string                  `json:"to"`          // "YYYY-MM-DD"
	WarmupBars int                     `json:"warmup_bars"` // bars to prime before recording; default 100
	Strategy   strategy.StrategyConfig `json:"strategy"`
	Exit       strategy.ExitConfig     `json:"exit"`
	Regime     strategy.RegimeConfig   `json:"regime"`
}

// ── replayPosition tracks open simulated positions during replay ──────────────

type replayPosition struct {
	id           string
	side         trader.Side
	entryPrice   trader.Price
	currentStop  trader.Price
	extremePrice trader.Price
	openTime     trader.Timestamp
}

// ── RunReplay ────────────────────────────────────────────────────────────────

// RunReplay runs a strategy against stored candles and returns every bar plus
// every signal the strategy emitted. The first WarmupBars bars prime the
// indicators without recording signals; the remainder are the live window.
func (s *Service) RunReplay(ctx context.Context, req ReplayRequest) (*ReplayResult, error) {
	inst := trader.NormalizeInstrument(req.Instrument)
	if inst == "" {
		return nil, fmt.Errorf("instrument is required")
	}

	tf := oandaGranToTF(req.Timeframe)
	if tf == trader.TF0 {
		return nil, fmt.Errorf("unsupported timeframe %q", req.Timeframe)
	}

	tr, err := trader.ParseTimeRange(req.From, req.To, req.Timeframe)
	if err != nil {
		return nil, fmt.Errorf("parse time range: %w", err)
	}

	warmup := req.WarmupBars
	if warmup <= 0 {
		warmup = 100
	}

	strat, err := strategy.GetStrategy(req.Strategy)
	if err != nil {
		return nil, fmt.Errorf("build strategy: %w", err)
	}

	exit, err := strategy.GetExitStrategy(req.Exit, trader.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("build exit strategy: %w", err)
	}

	regime, err := strategy.GetRegimeFilter(req.Regime, trader.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("build regime filter: %w", err)
	}

	// Load all bars. We include warmup bars before the requested range.
	fromWithWarmup := tr.Start.Time().Add(-warmupDuration(req.Timeframe, warmup))
	dm := marketdata.NewDataManager([]string{inst}, fromWithWarmup, tr.End.Time())
	iter, err := dm.Candles(ctx, marketdata.CandleRequest{
		Source:     trader.SourceOanda,
		Instrument: inst,
		Range: trader.TimeRange{
			Start: trader.FromTime(fromWithWarmup),
			End:   tr.End,
			TF:    tf,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("load candles: %w", err)
	}
	defer func() { _ = iter.Close() }()

	scale := float64(trader.PriceScale)
	inst_ := trader.GetInstrument(inst)

	var (
		bars      []ReplayCandleBar
		signals   []Signal
		positions []*replayPosition
		barIdx    int
	)

	bt := &trader.Backtest{
		Request: &trader.BacktestRequest{Instrument: inst},
		State:   &trader.BacktestRun{Lots: &execution.LotBook{}},
	}

	for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		candle := ct.Candle
		ts := ct.Timestamp

		// Tick indicators.
		regime.Tick(ct)
		exit.Tick(candle)

		// Always collect bars in the requested range (not warmup).
		inRange := !ts.Time().Before(tr.Start.Time())
		if inRange {
			bars = append(bars, ReplayCandleBar{
				Time:  int64(ts),
				Open:  candle.Open.Float64(),
				High:  candle.High.Float64(),
				Low:   candle.Low.Float64(),
				Close: candle.Close.Float64(),
			})
		}

		barIdx++

		// Update trailing stops on open positions.
		if inRange && exit.Ready() {
			for _, pos := range positions {
				switch pos.side {
				case trader.Long:
					if ct.High > pos.extremePrice {
						pos.extremePrice = ct.High
					}
				case trader.Short:
					if pos.extremePrice == 0 || ct.Low < pos.extremePrice {
						pos.extremePrice = ct.Low
					}
				}
				newStop := exit.UpdateStop(pos.side, pos.currentStop, pos.entryPrice, pos.extremePrice, candle)
				if newStop != 0 && newStop != pos.currentStop {
					signals = append(signals, Signal{
						Time:      int64(ts),
						Kind:      SignalStopUpdate,
						Side:      sideStr(pos.side),
						StopPrice: float64(newStop) / scale,
						StopPips:  pipsFromDist(pos.side, candle.Close, newStop, inst_),
					})
					pos.currentStop = newStop
				}
			}
		}

		// Run strategy.
		plan := strat.Update(ctx, &ct, bt)
		if plan == nil || (!inRange) {
			continue
		}

		// Process closes.
		for _, cl := range plan.Closes {
			// Determine side from the closed lot if available, else from our tracker.
			side := trader.Long
			if cl.Lot != nil {
				side = cl.Lot.Side
				bt.State.Lots.Delete(cl.Lot.ID)
				// Also remove from our position tracker.
				for i, pos := range positions {
					if pos.id == cl.Lot.ID {
						positions = append(positions[:i], positions[i+1:]...)
						break
					}
				}
			} else if len(positions) > 0 {
				side = positions[0].side
				bt.State.Lots.Delete(positions[0].id)
				positions = positions[1:]
			}
			signals = append(signals, Signal{
				Time:   int64(ts),
				Kind:   SignalClose,
				Side:   sideStr(side),
				Price:  candle.Close.Float64(),
				Reason: plan.Reason,
			})
		}

		// Process opens.
		for _, op := range plan.Opens {
			stop := op.Stop

			// Mirror the backtest loop: ask exit strategy for initial stop if not set.
			if stop == 0 && exit.Ready() {
				stop = exit.InitialStop(op.Side, candle.Close, candle)
			}

			if stop == 0 {
				signals = append(signals, Signal{
					Time:   int64(ts),
					Kind:   SignalNoStop,
					Side:   sideStr(op.Side),
					Price:  candle.Close.Float64(),
					Reason: plan.Reason,
				})
				continue
			}

			// Apply regime filter (mirrors trader.go and CandleStrategyAdapter).
			if regime.Ready() && !regime.Trending() {
				signals = append(signals, Signal{
					Time:   int64(ts),
					Kind:   SignalBlocked,
					Side:   sideStr(op.Side),
					Price:  candle.Close.Float64(),
					Reason: "regime: not trending",
				})
				continue
			}
			if regime.Ready() && !regime.AllowSide(op.Side) {
				signals = append(signals, Signal{
					Time:   int64(ts),
					Kind:   SignalBlocked,
					Side:   sideStr(op.Side),
					Price:  candle.Close.Float64(),
					Reason: "regime: side not allowed",
				})
				continue
			}

			stopPips := pipsFromDist(op.Side, candle.Close, stop, inst_)
			posID := trader.NewULID()
			signals = append(signals, Signal{
				Time:      int64(ts),
				Kind:      SignalOpen,
				Side:      sideStr(op.Side),
				Price:     candle.Close.Float64(),
				StopPrice: float64(stop) / scale,
				StopPips:  stopPips,
				Reason:    plan.Reason,
			})

			// Add to the synthetic LotBook so the strategy can see open positions.
			tc := &execution.TradeCommon{ID: posID}
			tc.Side = op.Side
			tc.Stop = stop
			tc.Instrument = inst
			bt.State.Lots.Add(&execution.Lot{
				TradeCommon:    tc,
				EntryPrice:     candle.Close,
				OriginalUnits:  1,
				RemainingUnits: 1,
				State:          execution.LotOpen,
			})

			positions = append(positions, &replayPosition{
				id:           posID,
				side:         op.Side,
				entryPrice:   candle.Close,
				currentStop:  stop,
				extremePrice: candle.Close,
				openTime:     ts,
			})
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("iterate candles: %w", err)
	}

	return &ReplayResult{
		Instrument: inst,
		Timeframe:  req.Timeframe,
		Strategy:   strat.Name(),
		From:       req.From,
		To:         req.To,
		WarmupBars: warmup,
		Bars:       bars,
		Signals:    signals,
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func sideStr(s trader.Side) string {
	if s == trader.Short {
		return "short"
	}
	return "long"
}

func pipsFromDist(_ trader.Side, entry, stop trader.Price, inst *trader.Instrument) float64 {
	if inst == nil {
		return 0
	}
	dist := entry - stop
	if dist < 0 {
		dist = -dist
	}
	perPip := inst.PriceUnitsPerPip()
	if perPip <= 0 {
		return 0
	}
	return math.Round(float64(dist)/float64(perPip)*10) / 10
}

// warmupDuration converts n bars of the given granularity to a time.Duration
// with a 1.4× weekend/holiday buffer, matching barsBefore logic.
func warmupDuration(granularity string, n int) time.Duration {
	var unit time.Duration
	switch granularity {
	case "D", "D1":
		unit = 24 * time.Hour
	case "H1":
		unit = time.Hour
	case "H4":
		unit = 4 * time.Hour
	default:
		unit = time.Minute
	}
	return time.Duration(float64(time.Duration(n)*unit) * 1.4)
}
