package replaysvc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/idgen"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
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
	side         types.Side
	entryPrice   types.Price
	currentStop  types.Price
	extremePrice types.Price
	openTime     types.Timestamp
}

// Service holds replay's (empty) dependency set. RunReplay reads entirely
// through datamanager and has no OANDA/Log/account dependency, unlike
// backtestsvc/datasvc/reviewsvc.
type Service struct{}

// oandaGranToTF converts an OANDA granularity string ("H1", "D", "M1") to
// the internal types.Timeframe constant used by the candle store.
//
// Deliberately duplicated from service/candle_strategy_adapter.go's
// identical helper rather than shared: it's a 5-line pure switch, and a
// shared dependency would mean this analysis-cluster package importing
// something from the live-trading cluster (or vice versa) for a function
// this trivial — not worth the coupling.
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

// ── RunReplay ────────────────────────────────────────────────────────────────

// RunReplay runs a strategy against stored candles and returns every bar plus
// every signal the strategy emitted. The first WarmupBars bars prime the
// indicators without recording signals; the remainder are the live window.
func (s *Service) RunReplay(ctx context.Context, req ReplayRequest) (*ReplayResult, error) {
	inst := market.NormalizeInstrument(req.Instrument)
	if inst == "" {
		return nil, fmt.Errorf("instrument is required")
	}

	tf := oandaGranToTF(req.Timeframe)
	if tf == types.TF0 {
		return nil, fmt.Errorf("unsupported timeframe %q", req.Timeframe)
	}

	tr, err := types.ParseTimeRange(req.From, req.To, req.Timeframe)
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

	exit, err := strategy.GetExitStrategy(req.Exit, types.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("build exit strategy: %w", err)
	}

	regime, err := strategy.GetRegimeFilter(req.Regime, types.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("build regime filter: %w", err)
	}

	// Load all bars. We include warmup bars before the requested range.
	fromWithWarmup := tr.Start.Time().Add(-warmupDuration(req.Timeframe, warmup))
	dm := datamanager.NewDataManager([]string{inst}, fromWithWarmup, tr.End.Time())
	iter, err := dm.Candles(ctx, datamanager.CandleRequest{
		Source:     market.SourceOanda,
		Instrument: inst,
		Range: types.TimeRange{
			Start: types.FromTime(fromWithWarmup),
			End:   tr.End,
			TF:    tf,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("load candles: %w", err)
	}
	defer func() { _ = iter.Close() }()

	inst_ := market.GetInstrument(inst)

	var (
		bars      []ReplayCandleBar
		signals   []Signal
		positions []*replayPosition
		barIdx    int
	)

	bt := &backtest.Backtest{
		Request: &backtest.BacktestRequest{Instrument: inst},
		State:   &backtest.BacktestRun{Lots: &account.LotBook{}},
	}

	for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		candle := ct
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
				case types.Long:
					if ct.High > pos.extremePrice {
						pos.extremePrice = ct.High
					}
				case types.Short:
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
						StopPrice: newStop.Float64(),
						StopPips:  pipsFromDist(pos.side, candle.Close, newStop, inst_),
					})
					pos.currentStop = newStop
				}
			}
		}

		// Run strategy — now returns a Signal (pure intent).
		sig := strat.Update(ctx, &ct, bt)
		if !inRange {
			continue
		}

		// Process closes: CloseAll closes every open position; directional signals
		// also close the opposing side (reversal).
		var toClose []*replayPosition
		if sig.CloseAll {
			toClose = append(toClose, positions...)
		} else if sig.Side != types.Flat {
			for _, pos := range positions {
				if pos.side != sig.Side {
					toClose = append(toClose, pos)
				}
			}
		}
		for _, pos := range toClose {
			bt.State.Lots.Delete(pos.id)
			remaining := positions[:0]
			for _, p := range positions {
				if p.id != pos.id {
					remaining = append(remaining, p)
				}
			}
			positions = remaining
			signals = append(signals, Signal{
				Time:   int64(ts),
				Kind:   SignalClose,
				Side:   sideStr(pos.side),
				Price:  candle.Close.Float64(),
				Reason: sig.Reason,
			})
		}

		// Process open: emit if the signal is directional.
		if sig.Side != types.Flat {
			stop := types.Price(0)
			if exit.Ready() {
				stop = exit.InitialStop(sig.Side, candle.Close, candle)
			}

			if stop == 0 {
				signals = append(signals, Signal{
					Time:   int64(ts),
					Kind:   SignalNoStop,
					Side:   sideStr(sig.Side),
					Price:  candle.Close.Float64(),
					Reason: sig.Reason,
				})
			} else {
				// Apply regime filter.
				if regime.Ready() && !regime.Trending() {
					signals = append(signals, Signal{
						Time:   int64(ts),
						Kind:   SignalBlocked,
						Side:   sideStr(sig.Side),
						Price:  candle.Close.Float64(),
						Reason: "regime: not trending",
					})
				} else if regime.Ready() && !regime.AllowSide(sig.Side) {
					signals = append(signals, Signal{
						Time:   int64(ts),
						Kind:   SignalBlocked,
						Side:   sideStr(sig.Side),
						Price:  candle.Close.Float64(),
						Reason: "regime: side not allowed",
					})
				} else {
					stopPips := pipsFromDist(sig.Side, candle.Close, stop, inst_)
					posID := idgen.NewULID()
					signals = append(signals, Signal{
						Time:      int64(ts),
						Kind:      SignalOpen,
						Side:      sideStr(sig.Side),
						Price:     candle.Close.Float64(),
						StopPrice: stop.Float64(),
						StopPips:  stopPips,
						Reason:    sig.Reason,
					})

					tc := &account.TradeCommon{ID: posID}
					tc.Side = sig.Side
					tc.Stop = stop
					tc.Instrument = inst
					bt.State.Lots.Add(&account.Lot{
						TradeCommon:    tc,
						EntryPrice:     candle.Close,
						OriginalUnits:  1,
						RemainingUnits: 1,
						State:          account.LotOpen,
					})

					positions = append(positions, &replayPosition{
						id:           posID,
						side:         sig.Side,
						entryPrice:   candle.Close,
						currentStop:  stop,
						extremePrice: candle.Close,
						openTime:     ts,
					})
				}
			}
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

func sideStr(s types.Side) string {
	if s == types.Short {
		return "short"
	}
	return "long"
}

func pipsFromDist(_ types.Side, entry, stop types.Price, inst *market.Instrument) float64 {
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
	tenthPips := (int64(dist)*10 + int64(perPip)/2) / int64(perPip)
	return float64(tenthPips) / 10.0
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
