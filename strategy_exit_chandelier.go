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
	multiplier float64
}

func NewChandelierExit(atrPeriod int, multiplier float64, scale Scale6) (*ChandelierExit, error) {
	atr, err := NewATR(atrPeriod, scale)
	if err != nil {
		return nil, err
	}
	return &ChandelierExit{
		atr:        atr,
		multiplier: multiplier,
	}, nil
}

func (c *ChandelierExit) Name() string {
	return fmt.Sprintf("Chandelier(ATR%d×%.1f)", c.atr.n, c.multiplier)
}

func (c *ChandelierExit) Ready() bool { return c.atr.Ready() }

func (c *ChandelierExit) Tick(candle Candle) { c.atr.Update(candle) }

func (c *ChandelierExit) InitialStop(side Side, entry Price, candle Candle) Price {
	if !c.atr.Ready() {
		return 0
	}
	offset := Price(c.atr.Float64() * c.multiplier * float64(PriceScale))
	switch side {
	case Long:
		stop := entry - offset
		if stop < 0 {
			return 0
		}
		return stop
	case Short:
		return entry + offset
	}
	return 0
}

func (c *ChandelierExit) UpdateStop(side Side, currentStop Price, _ Price, extreme Price, candle Candle) Price {
	if !c.atr.Ready() {
		return currentStop
	}
	offset := Price(c.atr.Float64() * c.multiplier * float64(PriceScale))

	switch side {
	case Long:
		// Update extreme to highest high
		newExtreme := extreme
		if candle.High > newExtreme || newExtreme == 0 {
			newExtreme = candle.High
		}
		candidate := newExtreme - offset
		if candidate > currentStop {
			return candidate
		}
	case Short:
		// Update extreme to lowest low
		newExtreme := extreme
		if candle.Low < newExtreme || newExtreme == 0 {
			newExtreme = candle.Low
		}
		candidate := newExtreme + offset
		if currentStop == 0 || candidate < currentStop {
			return candidate
		}
	}
	return currentStop
}
