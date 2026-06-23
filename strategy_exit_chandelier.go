package trader

import "fmt"

// ChandelierExit trails the stop from the highest-high (long) or lowest-low
// (short) seen since entry, offset by N×ATR. The stop only ever moves in the
// profitable direction — it never moves against the position.
//
// Per-position extreme tracking lives on Lot.ExtremePrice so multiple
// concurrent lots each maintain their own watermark.
type ChandelierExit struct {
	atr        *ATR
	multiplier Units // fixed-point; use UnitsFromFloat at construction, Float64() at output boundaries
}

func NewChandelierExit(atrPeriod int, multiplier float64, scale Scale6) (*ChandelierExit, error) {
	atr, err := NewATR(atrPeriod, scale)
	if err != nil {
		return nil, err
	}
	return &ChandelierExit{
		atr:        atr,
		multiplier: UnitsFromFloat(multiplier),
	}, nil
}

func (c *ChandelierExit) Name() string {
	// Convert back to float64 only for display.
	return fmt.Sprintf("Chandelier(ATR%d×%.1f)", c.atr.Period(), c.multiplier.Float64())
}

func (c *ChandelierExit) Ready() bool { return c.atr.Ready() }

func (c *ChandelierExit) Tick(candle Candle) { c.atr.Update(candle) }

// atrOffset returns the ATR-based stop offset in scaled Price units.
// All arithmetic is integer; no float64 round-trip.
func (c *ChandelierExit) atrOffset() Price {
	return Price(roundDivPositive(int64(c.atr.PriceSum())*int64(c.multiplier), UnitsScale))
}

func (c *ChandelierExit) InitialStop(side Side, entry Price, candle Candle) Price {
	if !c.atr.Ready() {
		return 0
	}
	offset := c.atrOffset()
	switch side {
	case Long:
		stop := entry - offset
		// entry - offset can underflow for very large ATR values; clamp to zero.
		if stop < 0 {
			return 0
		}
		return stop
	case Short:
		// Short stop is always above entry, so no underflow risk.
		return entry + offset
	}
	return 0
}

func (c *ChandelierExit) UpdateStop(side Side, currentStop Price, _ Price, extreme Price, candle Candle) Price {
	if !c.atr.Ready() {
		return currentStop
	}
	offset := c.atrOffset()

	// extreme is already the current-bar watermark advanced by the caller
	// (highest-high for Long, lowest-low for Short). Use it directly.
	switch side {
	case Long:
		candidate := extreme - offset
		if candidate > currentStop {
			return candidate
		}
	case Short:
		candidate := extreme + offset
		if currentStop == 0 || candidate < currentStop {
			return candidate
		}
	}
	return currentStop
}
