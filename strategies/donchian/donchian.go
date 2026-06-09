// Package donchian implements the Donchian breakout strategy with
// close-strength confirmation. Registers itself with the trader strategy
// registry under "donchian" and "donchian-breakout".
package donchian

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader"
)

func init() {
	trader.RegisterStrategy(build, "donchian", "donchian-breakout")
}

// Breakout enters when the close breaks above the N-bar high (long) or
// below the N-bar low (short), with a close-strength confirmation filter
// to reject weak/wick breakouts.
//
// The N-bar window is the COMPLETED bars preceding the current one — the
// current bar's high/low does not contaminate the channel.
//
// No internal stop is set; pair with an ExitStrategy (e.g. ChandelierExit)
// to manage trailing stops.
type Breakout struct {
	period        int
	closeStrength float64
	allowStacking bool

	// Circular buffer of completed-bar highs and lows.
	highs []trader.Price
	lows  []trader.Price
	pos   int
	count int

	name string
}

type Config struct {
	trader.StrategyBaseConfig

	Period        int     // N-bar lookback (e.g. 20)
	CloseStrength float64 // 0.5 = no filter; 0.6 = close in upper/lower 40% of bar
	AllowStacking bool    // true allows repeated same-direction entries
}

func New(cfg Config) (*Breakout, error) {
	if cfg.Period <= 1 {
		return nil, fmt.Errorf("donchian: period must be > 1")
	}
	if cfg.CloseStrength < 0.5 || cfg.CloseStrength > 1.0 {
		return nil, fmt.Errorf("donchian: close_strength must be in [0.5, 1.0]")
	}
	name := fmt.Sprintf("DONCHIAN(%d,cs=%.2f)", cfg.Period, cfg.CloseStrength)
	if cfg.AllowStacking {
		name = fmt.Sprintf("DONCHIAN(%d,cs=%.2f,stack)", cfg.Period, cfg.CloseStrength)
	}
	return &Breakout{
		period:        cfg.Period,
		closeStrength: cfg.CloseStrength,
		allowStacking: cfg.AllowStacking,
		highs:         make([]trader.Price, cfg.Period),
		lows:          make([]trader.Price, cfg.Period),
		name:          name,
	}, nil
}

func (d *Breakout) Name() string            { return d.name }
func (d *Breakout) StopDescription() string { return "" } // delegated to ExitStrategy

func (d *Breakout) Reset() {
	for i := range d.highs {
		d.highs[i] = 0
		d.lows[i] = 0
	}
	d.pos = 0
	d.count = 0
}

func (d *Breakout) Ready() bool { return d.count >= d.period }

// channelHighLow returns the highest high and lowest low across the buffer.
// Caller must ensure Ready().
func (d *Breakout) channelHighLow() (trader.Price, trader.Price) {
	hi := d.highs[0]
	lo := d.lows[0]
	for i := 1; i < d.period; i++ {
		if d.highs[i] > hi {
			hi = d.highs[i]
		}
		if d.lows[i] < lo {
			lo = d.lows[i]
		}
	}
	return hi, lo
}

// pushBar appends the just-closed bar to the rolling window.
func (d *Breakout) pushBar(c trader.Candle) {
	d.highs[d.pos] = c.High
	d.lows[d.pos] = c.Low
	d.pos = (d.pos + 1) % d.period
	if d.count < d.period {
		d.count++
	}
}

// closeStrengthOK reports whether the breakout bar's close sits in the
// favorable portion of the bar for the breakout direction.
func closeStrengthOK(c trader.Candle, side trader.Side, threshold float64) bool {
	rng := float64(c.High - c.Low)
	if rng <= 0 {
		return false
	}
	if side == trader.Long {
		return float64(c.Close-c.Low)/rng >= threshold
	}
	return float64(c.High-c.Close)/rng >= threshold
}

func (d *Breakout) Update(ctx context.Context, ct *trader.CandleTime, run *trader.Backtest) *trader.StrategyPlan {
	_ = ctx
	if ct == nil {
		return &trader.DefaultStrategyPlan
	}

	if !d.Ready() {
		d.pushBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "warming up"}
	}

	hi, lo := d.channelHighLow()

	var side trader.Side
	switch {
	case ct.Close > hi:
		side = trader.Long
	case ct.Close < lo:
		side = trader.Short
	default:
		d.pushBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "no breakout"}
	}

	if !closeStrengthOK(ct.Candle, side, d.closeStrength) {
		d.pushBar(ct.Candle)
		return &trader.StrategyPlan{Reason: "weak close"}
	}

	plan := emitOpen(ct, run, side, d.allowStacking)
	d.pushBar(ct.Candle)
	if side == trader.Long {
		plan.Reason = "donchian-breakout-up"
	} else {
		plan.Reason = "donchian-breakout-down"
	}
	return plan
}

// emitOpen closes any opposite open lots, then opens a new position.
// If stacking is disabled and a position in the same direction is already open,
// no new entry is emitted.
// No internal stop is set — the ExitStrategy supplies one via InitialStop.
func emitOpen(ct *trader.CandleTime, run *trader.Backtest, side trader.Side, allowStacking bool) *trader.StrategyPlan {
	plan := &trader.StrategyPlan{}

	alreadyOpen := false
	if run != nil && run.State != nil && run.State.Lots != nil {
		_ = run.State.Lots.Range(func(lot *trader.Lot) error {
			if lot.State != trader.LotOpen {
				return nil
			}
			if lot.Side == side {
				if !allowStacking {
					// Already in this direction — skip new entry.
					alreadyOpen = true
				}
				return nil
			}
			plan.Closes = append(plan.Closes, &trader.CloseRequest{
				Request: trader.Request{
					TradeCommon: lot.TradeCommon,
					Reason:      "donchian-reverse",
					Candle:      ct.Candle,
					RequestType: trader.RequestClose,
					Price:       ct.Close,
					Timestamp:   ct.Timestamp,
				},
				Lot:        lot,
				CloseCause: trader.CloseManual,
			})
			return nil
		})
	}

	if alreadyOpen {
		return plan
	}

	inst := ""
	if run != nil && run.Request != nil {
		inst = run.Request.Instrument
	}
	plan.Opens = append(plan.Opens, trader.NewOpenRequest(inst, ct, side, 0, 0, "donchian-breakout"))
	return plan
}

// build is the registry constructor. Reads "period", "close_strength", and
// "single_position" from params and returns a configured Breakout.
func build(params map[string]any) (trader.Strategy, error) {
	period, _, err := trader.GetInt32Param(params, "period")
	if err != nil {
		return nil, err
	}
	if period <= 1 {
		period = 20
	}
	closeStrength, ok, err := trader.GetFloat64Param(params, "close_strength")
	if err != nil {
		return nil, err
	}
	if !ok {
		closeStrength = 0.6
	}
	singlePosition, ok, err := trader.GetBoolParam(params, "single_position")
	if err != nil {
		return nil, err
	}
	if !ok {
		singlePosition = true
	}
	return New(Config{
		Period:        int(period),
		CloseStrength: closeStrength,
		AllowStacking: !singlePosition,
	})
}
