// Package bollingerfade implements a Bollinger Band mean-reversion strategy.
//
// Entry: close crosses outside the outer band — long below the lower band,
// short above the upper band.
//
// Exit: signal-based — close when the bar closes back beyond the middle band
// (SMA) in the expected direction. A hard ATR stop protects against
// runaway trends.
//
// One position at a time; no auto-reversals. The regime filter (e.g.
// choppiness-d1) in the backtest config should gate entries to ranging periods.
//
// Registers under "bb-fade" and "bollinger-fade".
package bollingerfade

import (
	"context"
	"fmt"
	"math"

	"github.com/rustyeddy/trader"
)

func init() {
	trader.RegisterStrategy(build, "bb-fade", "bollinger-fade")
}

// Fade is the Bollinger Band fade strategy.
type Fade struct {
	bb      *trader.BollingerBands
	atr     *trader.ATR
	atrMult float64
	scale   float64
	name    string
}

// Config holds constructor parameters.
type Config struct {
	trader.StrategyBaseConfig

	Period     int
	Multiplier float64
	ATRPeriod  int
	ATRMult    float64
}

func New(cfg Config) (*Fade, error) {
	period := cfg.Period
	if period < 2 {
		period = 20
	}
	mult := cfg.Multiplier
	if mult <= 0 {
		mult = 2.0
	}
	atrPeriod := cfg.ATRPeriod
	if atrPeriod <= 0 {
		atrPeriod = 14
	}
	atrMult := cfg.ATRMult
	if atrMult <= 0 {
		atrMult = 1.5
	}
	bb, err := trader.NewBollingerBands(period, mult, trader.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("bb-fade: Bollinger Bands: %w", err)
	}
	atr, err := trader.NewATR(atrPeriod, trader.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("bb-fade: ATR: %w", err)
	}
	return &Fade{
		bb:      bb,
		atr:     atr,
		atrMult: atrMult,
		scale:   float64(trader.PriceScale),
		name:    fmt.Sprintf("BB-FADE(%d,%.1f,atr=%d×%.1f)", period, mult, atrPeriod, atrMult),
	}, nil
}

func (f *Fade) Name() string            { return f.name }
func (f *Fade) StopDescription() string { return fmt.Sprintf("ATR×%.1f", f.atrMult) }
func (f *Fade) Ready() bool             { return f.bb.Ready() && f.atr.Ready() }

func (f *Fade) Reset() {
	f.bb.Reset()
	f.atr.Reset()
}

func (f *Fade) Update(ctx context.Context, ct *trader.CandleTime, run *trader.Backtest) *trader.StrategyPlan {
	_ = ctx
	if ct == nil {
		return &trader.DefaultStrategyPlan
	}

	f.bb.Update(ct.Candle)
	f.atr.Update(ct.Candle)

	if !f.Ready() {
		return &trader.StrategyPlan{Reason: "warming up"}
	}

	middle := f.bb.MiddlePrice()
	lower := f.bb.LowerPrice()
	upper := f.bb.UpperPrice()

	plan := &trader.StrategyPlan{}

	// Check open lots: close any that have reverted to the middle band.
	hasOpen := false
	if run != nil && run.State != nil && run.State.Lots != nil {
		_ = run.State.Lots.Range(func(lot *trader.Lot) error {
			if lot.State != trader.LotOpen {
				return nil
			}
			hasOpen = true
			var shouldClose bool
			switch lot.Side {
			case trader.Long:
				shouldClose = ct.Close >= middle
			case trader.Short:
				shouldClose = ct.Close <= middle
			}
			if shouldClose {
				plan.Closes = append(plan.Closes, &trader.CloseRequest{
					Request: trader.Request{
						TradeCommon: lot.TradeCommon,
						Reason:      "bb-revert",
						Candle:      ct.Candle,
						RequestType: trader.RequestClose,
						Price:       ct.Close,
						Timestamp:   ct.Timestamp,
					},
					Lot:        lot,
					CloseCause: trader.CloseManual,
				})
			}
			return nil
		})
	}

	if hasOpen {
		return plan
	}

	// Entry signals — only when no position is open.
	atrPrice := trader.Price(math.Round(f.atrMult * f.atr.Float64() * f.scale))

	switch {
	case ct.Close < lower:
		// Price closed below lower band — fade the drop, expect reversion up.
		stop := ct.Close - atrPrice
		open := trader.NewOpenRequest(instrumentFrom(run), ct, trader.Long, stop, 0, "bb-fade-long")
		plan.Opens = append(plan.Opens, open)
		plan.Reason = fmt.Sprintf("bb-fade-long(close=%.5f<lower=%.5f)",
			float64(ct.Close)/f.scale, float64(lower)/f.scale)

	case ct.Close > upper:
		// Price closed above upper band — fade the rise, expect reversion down.
		stop := ct.Close + atrPrice
		open := trader.NewOpenRequest(instrumentFrom(run), ct, trader.Short, stop, 0, "bb-fade-short")
		plan.Opens = append(plan.Opens, open)
		plan.Reason = fmt.Sprintf("bb-fade-short(close=%.5f>upper=%.5f)",
			float64(ct.Close)/f.scale, float64(upper)/f.scale)
	}

	return plan
}

func instrumentFrom(run *trader.Backtest) string {
	if run != nil && run.Request != nil {
		return run.Request.Instrument
	}
	return ""
}

func build(params map[string]any) (trader.Strategy, error) {
	period, _, err := trader.GetInt32Param(params, "period")
	if err != nil {
		return nil, err
	}
	mult, _, err := trader.GetFloat64Param(params, "multiplier")
	if err != nil {
		return nil, err
	}
	atrPeriod, _, err := trader.GetInt32Param(params, "atr_period")
	if err != nil {
		return nil, err
	}
	atrMult, _, err := trader.GetFloat64Param(params, "atr_mult")
	if err != nil {
		return nil, err
	}
	return New(Config{
		Period:     int(period),
		Multiplier: mult,
		ATRPeriod:  int(atrPeriod),
		ATRMult:    atrMult,
	})
}
