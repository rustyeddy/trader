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

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategy"
)

func init() {
	strategy.MustRegisterStrategy(build, "bb-fade", "bollinger-fade")
}

// Fade is the Bollinger Band fade strategy.
type Fade struct {
	bb      *indicator.BollingerBands
	atr     *indicator.ATR
	atrMult float64 // display only: stop description and strategy name
	name    string
}

// Config holds constructor parameters.
type Config struct {
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
	bb, err := indicator.NewBollingerBands(period, mult, market.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("bb-fade: Bollinger Bands: %w", err)
	}
	atr, err := indicator.NewATR(atrPeriod, market.PriceScale)
	if err != nil {
		return nil, fmt.Errorf("bb-fade: ATR: %w", err)
	}
	return &Fade{
		bb:      bb,
		atr:     atr,
		atrMult: atrMult,
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

func (f *Fade) Update(_ context.Context, ct *market.CandleTime, run strategy.StrategyContext) strategy.Signal {
	if ct == nil {
		return strategy.Hold("no candle")
	}

	f.bb.Update(ct.Candle)
	f.atr.Update(ct.Candle)

	if !f.Ready() {
		return strategy.Hold("warming up")
	}

	middle := f.bb.MiddlePrice()
	lower := f.bb.LowerPrice()
	upper := f.bb.UpperPrice()

	// Check open lots: close any that have reverted to the middle band.
	hasOpen := false
	shouldCloseAll := false
	if run != nil {
		_ = run.OpenLots().Range(func(lot *execution.Lot) error {
			if lot.State != execution.LotOpen {
				return nil
			}
			hasOpen = true
			switch lot.Side {
			case market.Long:
				if ct.Close >= middle {
					shouldCloseAll = true
				}
			case market.Short:
				if ct.Close <= middle {
					shouldCloseAll = true
				}
			}
			return nil
		})
	}

	if hasOpen {
		if shouldCloseAll {
			return strategy.Signal{CloseAll: true, Reason: "bb-revert"}
		}
		return strategy.Hold("holding position")
	}

	// Entry signals — only when no position is open.
	switch {
	case ct.Close < lower:
		return strategy.Signal{
			Side: market.Long,
			Reason: fmt.Sprintf("bb-fade-long(close=%.5f<lower=%.5f)",
				ct.Close.Float64(), lower.Float64()),
		}
	case ct.Close > upper:
		return strategy.Signal{
			Side: market.Short,
			Reason: fmt.Sprintf("bb-fade-short(close=%.5f>upper=%.5f)",
				ct.Close.Float64(), upper.Float64()),
		}
	}

	return strategy.Hold("no signal")
}

func build(params map[string]any) (strategy.Strategy, error) {
	period, _, err := strategy.GetInt32Param(params, "period")
	if err != nil {
		return nil, err
	}
	mult, _, err := strategy.GetFloat64Param(params, "multiplier")
	if err != nil {
		return nil, err
	}
	atrPeriod, _, err := strategy.GetInt32Param(params, "atr_period")
	if err != nil {
		return nil, err
	}
	atrMult, _, err := strategy.GetFloat64Param(params, "atr_mult")
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
