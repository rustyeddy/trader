package backtest

import (
	"math"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/pricing"
)

// H1EMACross is a minimal, candle-driven EMA cross strategy for H1 backtests.
// It enters on cross at bar close, with fixed units and fixed stop/target in pips.
type H1EMACross struct {
	Instrument string
	Fast       int
	Slow       int
	Units      int32
	StopPips   float64
	RR         float64

	// state
	fast EMAState
	slow EMAState

	lastDiff     float64
	haveLastDiff bool
}

type EMAState struct {
	period int
	alpha  float64
	value  float64
	count  int
}

func newEMAState(period int) EMAState {
	return EMAState{
		period: period,
		alpha:  2.0 / (float64(period) + 1.0),
	}
}

func (e *EMAState) Reset() {
	e.value = 0
	e.count = 0
}

func (e *EMAState) Update(x float64) {
	e.count++
	if e.count == 1 {
		e.value = x
		return
	}
	e.value = e.alpha*x + (1.0-e.alpha)*e.value
}

func (e *EMAState) Ready() bool {
	return e.count >= e.period
}

func (e *EMAState) Value() float64 { return e.value }

func NewH1EMACross(instrument string, fast, slow int, units int32, stopPips, rr float64) *H1EMACross {
	if rr <= 0 {
		rr = 2.0
	}
	return &H1EMACross{
		Instrument: instrument,
		Fast:       fast,
		Slow:       slow,
		Units:      units,
		StopPips:   stopPips,
		RR:         rr,
		fast:       newEMAState(fast),
		slow:       newEMAState(slow),
	}
}

func (s *H1EMACross) Name() string { return "H1-EMA-Cross" }

func (s *H1EMACross) Reset() {
	s.fast.Reset()
	s.slow.Reset()
	s.haveLastDiff = false
	s.lastDiff = 0
}

func (s *H1EMACross) OnBar(ctx *CandleContext, c pricing.Candle) *OrderRequest {
	if ctx == nil || ctx.CS == nil {
		return nil
	}
	if ctx.CS.Instrument != "" && s.Instrument != "" && ctx.CS.Instrument != s.Instrument {
		// single-instrument strategy
		return nil
	}
	if ctx.Pos != nil && ctx.Pos.Open {
		return nil
	}

	closeF := float64(c.C) / float64(ctx.CS.Scale)

	s.fast.Update(closeF)
	s.slow.Update(closeF)
	if !s.fast.Ready() || !s.slow.Ready() {
		return nil
	}

	diff := s.fast.Value() - s.slow.Value()
	if !s.haveLastDiff {
		s.lastDiff = diff
		s.haveLastDiff = true
		return nil
	}

	bullCross := diff > 0 && s.lastDiff <= 0
	bearCross := diff < 0 && s.lastDiff >= 0
	s.lastDiff = diff

	if !bullCross && !bearCross {
		return nil
	}

	meta, ok := market.Instruments[s.Instrument]
	if !ok {
		return nil
	}
	pipScaled, err := PipScaled(ctx.CS.Scale, meta.PipLocation)
	if err != nil {
		return nil
	}

	stopDist := int32(math.Round(s.StopPips * float64(pipScaled)))
	if stopDist <= 0 {
		return nil
	}
	takeDist := int32(math.Round(s.StopPips * s.RR * float64(pipScaled)))

	entry := c.C
	if bullCross {
		return &OrderRequest{
			Side:   Long,
			Units:  s.Units,
			Stop:   entry - stopDist,
			Take:   entry + takeDist,
			Reason: "BullCross",
		}
	}
	return &OrderRequest{
		Side:   Short,
		Units:  s.Units,
		Stop:   entry + stopDist,
		Take:   entry - takeDist,
		Reason: "BearCross",
	}
}
