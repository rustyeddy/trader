// Package scalper implements a "buy the dip" M1 scalper for live broker
// integration testing and incremental strategy development.
// Registers as "scalper" in the strategy registry.
package scalper

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/strategy"
)

func init() {
	strategy.MustRegisterStrategy(build, "scalper")
}

// Config holds scalper parameters.
type Config struct {
	FastPeriod     int
	SlowPeriod     int
	ATRPeriod      int
	StopMultiplier float64
}

// Strategy implements a buy-the-dip scalper on M1 candles.
//
// Signal logic:
//  1. Trend filter: close > slow EMA — only buy in an uptrend
//  2. Dip detection: close drops below fast EMA → dipSeen = true
//  3. Recovery entry: dipSeen AND close crosses back above fast EMA
//     AND close > open (bullish bar) → open long
//
// Stop = entry − ATR(atr_period) × stop_multiplier.
// Exit is delegated to hold_bars in the runner config.
type Strategy struct {
	cfg     Config
	name    string
	fastEMA *trader.EMA
	slowEMA *trader.EMA
	atr     *trader.ATR
	dipSeen bool
}

// New creates a Strategy. Returns an error for invalid config.
func New(cfg Config) (*Strategy, error) {
	if cfg.FastPeriod <= 0 {
		return nil, fmt.Errorf("scalper: fast_period must be > 0")
	}
	if cfg.SlowPeriod <= 0 {
		return nil, fmt.Errorf("scalper: slow_period must be > 0")
	}
	if cfg.FastPeriod >= cfg.SlowPeriod {
		return nil, fmt.Errorf("scalper: fast_period (%d) must be < slow_period (%d)",
			cfg.FastPeriod, cfg.SlowPeriod)
	}
	if cfg.ATRPeriod <= 0 {
		cfg.ATRPeriod = 14
	}
	if cfg.StopMultiplier <= 0 {
		cfg.StopMultiplier = 1.0
	}
	fastEMA, err := trader.NewEMA(cfg.FastPeriod, trader.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("scalper: fast EMA: %w", err)
	}
	slowEMA, err := trader.NewEMA(cfg.SlowPeriod, trader.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("scalper: slow EMA: %w", err)
	}
	atr, err := trader.NewATR(cfg.ATRPeriod, trader.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("scalper: ATR: %w", err)
	}
	return &Strategy{
		cfg:     cfg,
		name:    fmt.Sprintf("SCALPER(ema%d/%d,atr%d)", cfg.FastPeriod, cfg.SlowPeriod, cfg.ATRPeriod),
		fastEMA: fastEMA,
		slowEMA: slowEMA,
		atr:     atr,
	}, nil
}

func (s *Strategy) Name() string { return s.name }

func (s *Strategy) StopDescription() string {
	return fmt.Sprintf("ATR(%d)×%.1f", s.cfg.ATRPeriod, s.cfg.StopMultiplier)
}

func (s *Strategy) Ready() bool {
	return s.fastEMA.Ready() && s.slowEMA.Ready() && s.atr.Ready()
}

func (s *Strategy) Reset() {
	s.fastEMA.Reset()
	s.slowEMA.Reset()
	s.atr.Reset()
	s.dipSeen = false
}

// Update receives each completed M1 candle and returns a StrategyPlan.
func (s *Strategy) Update(ctx context.Context, ct *trader.CandleTime, run strategy.StrategyContext) *strategy.StrategyPlan {
	if ct == nil {
		return strategy.DefaultPlan()
	}

	c := ct.Candle
	s.fastEMA.Update(c)
	s.slowEMA.Update(c)
	s.atr.Update(c)

	if !s.Ready() {
		return &strategy.StrategyPlan{Reason: "warming up"}
	}

	// Skip if already in a position (netting account: one at a time).
	if run != nil && run.OpenLots().Len() > 0 {
		return &strategy.StrategyPlan{Reason: "in position"}
	}

	closePrice := trader.PriceSum(c.Close)
	openPrice := trader.PriceSum(c.Open)
	fastVal := s.fastEMA.PriceSum()
	slowVal := s.slowEMA.PriceSum()

	// Track dip: price closed below the fast EMA.
	if closePrice < fastVal {
		s.dipSeen = true
		return &strategy.StrategyPlan{Reason: "in dip"}
	}

	// Entry: dip recovery — bullish close back above fast EMA, in uptrend.
	if s.dipSeen && closePrice > fastVal && closePrice > openPrice && closePrice > slowVal {
		s.dipSeen = false
		stop := s.calcStop(ct)
		instr := ""
		if run != nil {
			instr = run.Instrument()
		}
		return &strategy.StrategyPlan{
			Opens:  []*execution.OpenRequest{execution.NewOpenRequest(instr, ct, trader.Long, stop, 0, "buy-the-dip")},
			Reason: "buy-the-dip",
		}
	}

	return &strategy.StrategyPlan{Reason: "no signal"}
}

// calcStop returns the stop price: entry − ATR × multiplier.
func (s *Strategy) calcStop(ct *trader.CandleTime) trader.Price {
	atrPrice := trader.Price(s.atr.Float64() * s.cfg.StopMultiplier * float64(trader.PriceScale))
	stop := ct.Close - atrPrice
	if stop <= 0 {
		stop = 1
	}
	return stop
}

func build(params map[string]any) (strategy.Strategy, error) {
	fast, _, err := strategy.GetInt32Param(params, "fast_period")
	if err != nil {
		return nil, err
	}
	if fast <= 0 {
		fast = 3
	}
	slow, _, err := strategy.GetInt32Param(params, "slow_period")
	if err != nil {
		return nil, err
	}
	if slow <= 0 {
		slow = 8
	}
	atrPeriod, _, err := strategy.GetInt32Param(params, "atr_period")
	if err != nil {
		return nil, err
	}
	if atrPeriod <= 0 {
		atrPeriod = 14
	}
	stopMult, _, _ := strategy.GetFloat64Param(params, "stop_multiplier")
	if stopMult <= 0 {
		stopMult = 1.0
	}
	return New(Config{
		FastPeriod:     int(fast),
		SlowPeriod:     int(slow),
		ATRPeriod:      int(atrPeriod),
		StopMultiplier: stopMult,
	})
}
