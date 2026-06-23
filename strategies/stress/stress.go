// Package stress implements an unconditional mechanical strategy that opens a
// trade every N candles with no indicator warmup. It fires at any time and on
// any instrument, making it the canonical API plumbing test for both backtest
// and live environments. Registers as "stress".
package stress

import (
	"context"
	"fmt"
	"strings"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/execution"
)

func init() {
	trader.MustRegisterStrategy(build, "stress")
}

// Config holds stress strategy parameters.
type Config struct {

	// TradeEvery opens a position every N candles (1 = every bar).
	TradeEvery int

	// StopBps is the stop-loss distance in basis points (1 bp = 0.01%).
	// e.g. 20 = 0.20%, 150 = 1.50%. Set from YAML stop_pct via build().
	StopBps int

	// Side controls direction: "long", "short", or "alternate".
	Side string
}

// Strategy fires a trade every TradeEvery candles. No indicators, no warmup.
type Strategy struct {
	cfg      Config
	candleN  int // candles since last trade was opened
	sideTurn int // for alternation; odd=Long, even=Short
}

// New creates a Strategy from the given Config.
func New(cfg Config) (*Strategy, error) {
	if cfg.TradeEvery <= 0 {
		cfg.TradeEvery = 1
	}
	if cfg.StopBps <= 0 {
		cfg.StopBps = 20 // 0.20% default
	}
	side := strings.ToLower(strings.TrimSpace(cfg.Side))
	if side == "" {
		side = "long"
	}
	cfg.Side = side
	return &Strategy{cfg: cfg}, nil
}

func (s *Strategy) Name() string {
	return fmt.Sprintf("STRESS(every=%d,side=%s,stop=%dbps)", s.cfg.TradeEvery, s.cfg.Side, s.cfg.StopBps)
}

func (s *Strategy) StopDescription() string {
	return fmt.Sprintf("%d bps (%.2f%%) of close", s.cfg.StopBps, float64(s.cfg.StopBps)/100)
}

func (s *Strategy) Ready() bool { return true }

func (s *Strategy) Reset() {
	s.candleN = 0
	s.sideTurn = 0
}

// Update is called on every completed candle. Returns an open request every
// TradeEvery candles when no position is already open.
func (s *Strategy) Update(ctx context.Context, ct *trader.CandleTime, run trader.StrategyContext) *trader.StrategyPlan {
	if ct == nil {
		return trader.DefaultPlan()
	}

	// Netting account: one position at a time.
	if run != nil && run.OpenLots().Len() > 0 {
		return &trader.StrategyPlan{Reason: "in position"}
	}

	s.candleN++
	if s.candleN < s.cfg.TradeEvery {
		return &trader.StrategyPlan{Reason: fmt.Sprintf("waiting (%d/%d)", s.candleN, s.cfg.TradeEvery)}
	}
	s.candleN = 0

	side := s.nextSide()
	stop := s.calcStop(ct, side)

	instr := ""
	if run != nil {
		instr = run.Instrument()
	}

	reason := fmt.Sprintf("stress-%s", side)
	return &trader.StrategyPlan{
		Opens:  []*execution.OpenRequest{execution.NewOpenRequest(instr, ct, side, stop, 0, reason)},
		Reason: reason,
	}
}

func (s *Strategy) nextSide() trader.Side {
	switch s.cfg.Side {
	case "long":
		return trader.Long
	case "short":
		return trader.Short
	default: // "alternate"
		s.sideTurn++
		if s.sideTurn%2 == 1 {
			return trader.Long
		}
		return trader.Short
	}
}

func (s *Strategy) calcStop(ct *trader.CandleTime, side trader.Side) trader.Price {
	dist := trader.Price(int64(ct.Close) * int64(s.cfg.StopBps) / 10000)
	if dist <= 0 {
		dist = 1
	}
	if side == trader.Long {
		stop := ct.Close - dist
		if stop <= 0 {
			stop = 1
		}
		return stop
	}
	return ct.Close + dist
}

// build is the registry factory. stop_pct is read as a human-friendly
// percentage (e.g. 0.2 = 0.2%, 1.5 = 1.5%) and converted to basis points
// once here so the strategy internals stay float-free.
func build(params map[string]any) (trader.Strategy, error) {
	tradeEvery, _, _ := trader.GetInt32Param(params, "trade_every")
	stopPct, _, _ := trader.GetFloat64Param(params, "stop_pct")
	side := ""
	if v, ok := params["side"]; ok {
		if s, ok := v.(string); ok {
			side = s
		}
	}
	return New(Config{
		TradeEvery: int(tradeEvery),
		StopBps:    int(stopPct * 100), // 0.2 → 20 bps, 1.5 → 150 bps
		Side:       side,
	})
}
