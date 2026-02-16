package backtest

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/market"
)

// Side: +1 long, -1 short
type Side int8

const (
	Long  Side = +1
	Short Side = -1
)

type Config struct {
	InitialBalance int64 // in "money cents" or "account units" (pick one)
	// Add slippage/spread models later if you want
	ResetIndicatorsOnGapHours int // 0 disables
}

type Trade struct {
	EntryTime  time.Time
	ExitTime   time.Time
	Side       Side
	EntryPrice int32 // scaled price units
	ExitPrice  int32
	Units      int32 // position size (your convention)
	PnLUnits   int64 // PnL in scaled price units * Units (or just scaled units, your choice)
	Reason     string
}

type Position struct {
	Open       bool
	Side       Side
	EntryPrice int32
	Units      int32
	Stop       int32 // scaled price, 0 means none
	Take       int32 // scaled price, 0 means none
	EntryIdx   int
	EntryTime  time.Time
}

type Engine struct {
	cs  *market.CandleSet
	cfg Config

	Balance int64
	Pos     Position
	Trades  []Trade
}

func NewEngine(cs *market.CandleSet, cfg Config) *Engine {
	return &Engine{
		cs:      cs,
		cfg:     cfg,
		Balance: cfg.InitialBalance,
	}
}

// Strategy is called once per valid bar (H1).
// It can update indicators, then optionally return an order request.
type Strategy interface {
	Name() string
	Reset()
	OnBar(ctx *Context, c market.Candle) *OrderRequest
}

// Context provides convenient access to time/index/price conversion.
type Context struct {
	CS      *market.CandleSet
	Idx     int
	Time    time.Time
	GapBars int // number of missing bars since previous valid bar (0 if contiguous)

	// Live state
	Pos     *Position
	Balance *int64
}

type OrderRequest struct {
	Side  Side
	Units int32

	// Optional risk params (scaled prices)
	Stop int32 // 0 means none
	Take int32 // 0 means none

	Reason string
}

// Run executes a backtest over the CandleSet using the strategy.
// CandleSet is expected to be H1 (Timeframe=3600), with iterator skipping invalid bars.
func (e *Engine) Run(strat Strategy) error {
	if e.cs == nil {
		return fmt.Errorf("nil CandleSet")
	}
	if e.cs.Timeframe != 3600 {
		return fmt.Errorf("expected H1 (3600s) CandleSet, got %d", e.cs.Timeframe)
	}

	strat.Reset()

	it := e.cs.Iterator()

	prevIdx := -1

	for it.Next() {
		idx := it.Index()
		t := it.Time()
		c := it.Candle()

		gapBars := 0
		if prevIdx != -1 {
			gapBars = idx - prevIdx - 1
		}
		prevIdx = idx

		// Optional: reset indicators / strategy state on large gaps (weekends)
		if e.cfg.ResetIndicatorsOnGapHours > 0 && gapBars > 0 {
			// gapBars is in hours for H1
			if gapBars >= e.cfg.ResetIndicatorsOnGapHours {
				strat.Reset()
			}
		}

		ctx := &Context{
			CS:      e.cs,
			Idx:     idx,
			Time:    t,
			GapBars: gapBars,
			Pos:     &e.Pos,
			Balance: &e.Balance,
		}

		// 1) Manage open position exits first (stop/take on this bar)
		if e.Pos.Open {
			if exitPx, reason, hit := checkExit(e.Pos, c); hit {
				e.closePosition(idx, t, exitPx, reason)
			}
		}

		// 2) Let strategy decide entries (or reversals later)
		req := strat.OnBar(ctx, c)
		if req == nil {
			continue
		}

		// Only one position at a time for now (simple engine)
		if e.Pos.Open {
			continue
		}

		e.openPosition(idx, t, c, req)
	}

	// Optional: close at last bar close
	// if e.Pos.Open { e.closePosition(prevIdx, e.cs.Time(prevIdx), lastClose, "EOD") }

	return nil
}

func (e *Engine) openPosition(idx int, t time.Time, c market.Candle, req *OrderRequest) {
	// Simple fill model: enter at bar close
	entry := c.C

	e.Pos = Position{
		Open:       true,
		Side:       req.Side,
		EntryPrice: entry,
		Units:      req.Units,
		Stop:       req.Stop,
		Take:       req.Take,
		EntryIdx:   idx,
		EntryTime:  t,
	}
}

func (e *Engine) closePosition(idx int, t time.Time, exit int32, reason string) {
	p := e.Pos
	e.Pos.Open = false

	// PnL in scaled price units * units
	// Long: (exit-entry) * units
	// Short: (entry-exit) * units  => same as side*(exit-entry) with side=+1/-1
	delta := int64(exit - p.EntryPrice)
	pnl := int64(p.Side) * delta * int64(p.Units)

	e.Trades = append(e.Trades, Trade{
		EntryTime:  p.EntryTime,
		ExitTime:   t,
		Side:       p.Side,
		EntryPrice: p.EntryPrice,
		ExitPrice:  exit,
		Units:      p.Units,
		PnLUnits:   pnl,
		Reason:     reason,
	})

	// Balance update policy is up to you. For now, keep it in "pnl units"
	// (You can later convert pnl units to dollars using instrument pip value.)
	e.Balance += pnl
}

// checkExit models stop/take hits within a candle.
// IMPORTANT: if both stop and take are hit in the same bar, we need a rule.
// For now: assume worst-case for the trader (stop first).
func checkExit(p Position, c market.Candle) (exitPx int32, reason string, hit bool) {
	if !p.Open {
		return 0, "", false
	}

	hasStop := p.Stop != 0
	hasTake := p.Take != 0

	switch p.Side {
	case Long:
		stopHit := hasStop && c.L <= p.Stop
		takeHit := hasTake && c.H >= p.Take

		if stopHit && takeHit {
			return p.Stop, "STOP&TAKE same bar (stop-first)", true
		}
		if stopHit {
			return p.Stop, "STOP", true
		}
		if takeHit {
			return p.Take, "TAKE", true
		}
	case Short:
		stopHit := hasStop && c.H >= p.Stop
		takeHit := hasTake && c.L <= p.Take

		if stopHit && takeHit {
			return p.Stop, "STOP&TAKE same bar (stop-first)", true
		}
		if stopHit {
			return p.Stop, "STOP", true
		}
		if takeHit {
			return p.Take, "TAKE", true
		}
	}

	return 0, "", false
}
