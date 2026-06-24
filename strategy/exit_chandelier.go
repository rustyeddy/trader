package strategy

import (
	"fmt"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
)

// ChandelierExit trails the stop from the highest-high (long) or lowest-low
// (short) seen since entry, offset by N×ATR. The stop only ever moves in the
// profitable direction — it never moves against the position.
//
// Per-position extreme tracking lives on Lot.ExtremePrice so multiple
// concurrent lots each maintain their own watermark.
type ChandelierExit struct {
	atr        *indicator.ATR
	multiplier market.Units // fixed-point; use UnitsFromFloat at construction, Float64() at output boundaries
}

func NewChandelierExit(atrPeriod int, multiplier float64, scale market.Scale6) (*ChandelierExit, error) {
	atr, err := indicator.NewATR(atrPeriod, scale)
	if err != nil {
		return nil, err
	}
	return &ChandelierExit{
		atr:        atr,
		multiplier: market.UnitsFromFloat(multiplier),
	}, nil
}

func (c *ChandelierExit) Name() string {
	// Convert back to float64 only for display.
	return fmt.Sprintf("Chandelier(ATR%d×%.1f)", c.atr.Period(), c.multiplier.Float64())
}

func (c *ChandelierExit) Ready() bool { return c.atr.Ready() }

func (c *ChandelierExit) Tick(candle market.Candle) { c.atr.Update(candle) }

// atrOffset returns the ATR-based stop offset in scaled Price units.
// All arithmetic is integer; no float64 round-trip.
func (c *ChandelierExit) atrOffset() market.Price {
	return market.Price(indicator.RoundDivPositive(int64(c.atr.PriceSum())*int64(c.multiplier), market.UnitsScale))
}

func (c *ChandelierExit) InitialStop(side market.Side, entry market.Price, candle market.Candle) market.Price {
	if !c.atr.Ready() {
		return 0
	}
	offset := c.atrOffset()
	switch side {
	case market.Long:
		stop := entry - offset
		// entry - offset can underflow for very large ATR values; clamp to zero.
		if stop < 0 {
			return 0
		}
		return stop
	case market.Short:
		// Short stop is always above entry, so no underflow risk.
		return entry + offset
	}
	return 0
}

func (c *ChandelierExit) UpdateStop(side market.Side, currentStop market.Price, _ market.Price, extreme market.Price, candle market.Candle) market.Price {
	if !c.atr.Ready() {
		return currentStop
	}
	offset := c.atrOffset()

	// extreme is already the current-bar watermark advanced by the caller
	// (highest-high for Long, lowest-low for Short). Use it directly.
	switch side {
	case market.Long:
		candidate := extreme - offset
		if candidate > currentStop {
			return candidate
		}
	case market.Short:
		candidate := extreme + offset
		if currentStop == 0 || candidate < currentStop {
			return candidate
		}
	}
	return currentStop
}
