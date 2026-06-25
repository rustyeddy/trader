// Package fake contains canned deterministic strategies used by trader's
// integration and lifecycle tests. Registers "fake" and "fake-02".
package fake

import (
	"context"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
)

func init() {
	strategy.MustRegisterStrategy(buildFake, "fake")
	strategy.MustRegisterStrategy(buildFake02, "fake-02")
}

// Fake opens long on higher highs.
type Fake struct {
	CandleCount int

	candles []*market.CandleTime
	highest market.Price
	lowest  market.Price
}

func (f *Fake) Name() string            { return "Fake" }
func (f *Fake) StopDescription() string { return "" }

func (f *Fake) Reset() {
	clear(f.candles)
	f.highest = 0
	f.lowest = 0
}

func (f *Fake) Ready() bool {
	return f.CandleCount == len(f.candles)
}

func (f *Fake) Update(_ context.Context, c *market.CandleTime, run strategy.StrategyContext) strategy.Signal {
	f.candles = append(f.candles, c)

	if len(f.candles) < f.CandleCount {
		return strategy.Hold("warming up")
	}

	if f.lowest == 0 || f.lowest > c.Low {
		f.lowest = c.Low
	}

	if f.highest < c.High {
		f.highest = c.High
		if run.OpenLots().Len() > 0 {
			return strategy.Hold("in position")
		}
		return strategy.Signal{Side: market.Long, Reason: "higher highs"}
	}

	return strategy.Hold("hold")
}

// Fake02 is a deterministic lifecycle/accounting test strategy.
type Fake02 struct {
	WaitBars int
	HoldBars int
	StopPips float64

	bar        int
	nextOpenAt int
	openedAt   int
	longNext   bool
}

func (f *Fake02) Name() string            { return "Fake02" }
func (f *Fake02) StopDescription() string { return "" }

func (f *Fake02) Reset() {
	f.bar = 0
	f.nextOpenAt = 0
	f.openedAt = 0
	f.longNext = false
}

func (f *Fake02) Ready() bool { return true }

func (f *Fake02) Update(_ context.Context, c *market.CandleTime, run strategy.StrategyContext) strategy.Signal {
	if c == nil {
		return strategy.Hold("hold")
	}

	if f.WaitBars <= 0 {
		f.WaitBars = 8
	}
	if f.HoldBars <= 0 {
		f.HoldBars = 6
	}
	if f.StopPips <= 0 {
		f.StopPips = 20
	}
	if f.nextOpenAt == 0 {
		f.nextOpenAt = 1
		f.longNext = true
	}

	f.bar++

	if run != nil && run.OpenLots().Len() > 0 {
		if (f.bar - f.openedAt) >= f.HoldBars {
			f.nextOpenAt = f.bar + f.WaitBars
			f.longNext = !f.longNext
			return strategy.Signal{CloseAll: true, Reason: "fake-02-close"}
		}
		return strategy.Hold("hold")
	}

	if f.bar < f.nextOpenAt {
		return strategy.Hold("hold")
	}

	side := market.Long
	if !f.longNext {
		side = market.Short
	}

	f.openedAt = f.bar
	return strategy.Signal{Side: side, Reason: "fake-02-open"}
}

func buildFake(params map[string]any) (strategy.Strategy, error) {
	return &Fake{CandleCount: 10}, nil
}

func buildFake02(params map[string]any) (strategy.Strategy, error) {
	return &Fake02{
		WaitBars: 8,
		HoldBars: 6,
	}, nil
}
