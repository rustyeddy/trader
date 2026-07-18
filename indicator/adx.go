package indicator

import (
	"fmt"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// ADX computes the Average Directional Index (Wilder) over candle OHLC.
//
// Readiness / warmup:
// - ADX needs:
//  1. N periods to build initial smoothed TR/+DM/-DM
//  2. N DX values to seed the initial ADX (average of first N DX)
//
// - Practically, that's about 2N "periods" (differences between candles), plus the first candle.
// - We expose Warmup() as 2N to keep it simple/consistent with your other indicators.
type ADX struct {
	n    int
	name string

	// candle tracking
	prev    market.Candle
	hasPrev bool
	ready   bool
	adx     int64
	plusDI  int64
	minusDI int64
	lastDX  int64
	periods int // number of computed periods (needs prev)

	// initial accumulation for first N periods
	sumTR      types.PriceSum
	sumPlusDM  types.PriceSum
	sumMinusDM types.PriceSum

	// Wilder smoothed values after initialization
	smTR      types.PriceSum
	smPlusDM  types.PriceSum
	smMinusDM types.PriceSum

	// seeding ADX: average of first N DX values
	dxSum   int64
	dxCount int
}

func NewADX(period int, scale types.Scale6) (*ADX, error) {
	if period <= 0 {
		return nil, fmt.Errorf("ADX period must be > 0")
	}
	if scale <= 0 {
		return nil, fmt.Errorf("ADX scale must be > 0")
	}
	return &ADX{
		n:    period,
		name: fmt.Sprintf("ADX(%d)", period),
	}, nil
}

func (a *ADX) Name() string     { return a.name }
func (a *ADX) Period() int      { return a.n }
func (a *ADX) Warmup() int      { return 2 * a.n }
func (a *ADX) Ready() bool      { return a.ready }
func (a *ADX) Float64() float64 { return fixedScaledToFloat64(a.adx) }

// Value returns the raw scaled ADX (×ValueScale). Use for fixed-point comparisons.
func (a *ADX) Value() int64 { return a.adx }

// ValueUnits returns ADX as types.Units (×UnitsScale == ×ValueScale).
// Use when comparing against a threshold stored as types.Units.
func (a *ADX) ValueUnits() types.Units { return types.Units(a.adx) }

// PlusDIRaw and MinusDIRaw return raw scaled DI values for fixed-point comparisons.
func (a *ADX) PlusDIRaw() int64  { return a.plusDI }
func (a *ADX) MinusDIRaw() int64 { return a.minusDI }

// PlusDIUnits and MinusDIUnits return DI values as types.Units.
func (a *ADX) PlusDIUnits() types.Units  { return types.Units(a.plusDI) }
func (a *ADX) MinusDIUnits() types.Units { return types.Units(a.minusDI) }

func (a *ADX) Reset() {
	*a = ADX{
		n:    a.n,
		name: a.name,
	}
}

// Update consumes the next closed candle.
func (a *ADX) Update(c market.Candle) {
	// Need a previous candle to form a "period"
	if !a.hasPrev {
		a.prev = c
		a.hasPrev = true
		return
	}

	tr := max3Int64(
		int64(c.High-c.Low),
		absPriceDiff(c.High, a.prev.Close),
		absPriceDiff(c.Low, a.prev.Close),
	)

	// Directional Movement
	upMove := int64(c.High - a.prev.High)
	downMove := int64(a.prev.Low - c.Low)

	var plusDM, minusDM int64
	if upMove > downMove && upMove > 0 {
		plusDM = upMove
	}
	if downMove > upMove && downMove > 0 {
		minusDM = downMove
	}

	// Advance period counter (period == one delta between candles)
	a.periods++

	// 1) Accumulate first N periods to initialize Wilder smoothing
	if a.periods <= a.n {
		a.sumTR += types.PriceSum(tr)
		a.sumPlusDM += types.PriceSum(plusDM)
		a.sumMinusDM += types.PriceSum(minusDM)

		// When we have N periods accumulated, initialize smoothed values
		if a.periods == a.n {
			a.smTR = a.sumTR
			a.smPlusDM = a.sumPlusDM
			a.smMinusDM = a.sumMinusDM

			a.plusDI, a.minusDI = diScaled(int64(a.smPlusDM), int64(a.smMinusDM), int64(a.smTR))
			dx := dxScaled(a.plusDI, a.minusDI)
			a.lastDX = dx

			a.dxSum = dx
			a.dxCount = 1
			// not ready yet: need N DX values to seed ADX
		}

		a.prev = c
		return
	}

	// 2) Wilder smoothing after initialization:
	// smoothed = prior_smoothed - (prior_smoothed / N) + current
	a.smTR = types.PriceSum(roundDivPositive(int64(a.smTR)*int64(a.n-1), int64(a.n)) + tr)
	a.smPlusDM = types.PriceSum(roundDivPositive(int64(a.smPlusDM)*int64(a.n-1), int64(a.n)) + plusDM)
	a.smMinusDM = types.PriceSum(roundDivPositive(int64(a.smMinusDM)*int64(a.n-1), int64(a.n)) + minusDM)

	a.plusDI, a.minusDI = diScaled(int64(a.smPlusDM), int64(a.smMinusDM), int64(a.smTR))
	dxVal := dxScaled(a.plusDI, a.minusDI)
	a.lastDX = dxVal

	// Seed ADX using the first N DX values, then Wilder-smooth ADX
	if !a.ready {
		a.dxSum += dxVal
		a.dxCount++
		if a.dxCount >= a.n {
			a.adx = roundDivPositive(a.dxSum, int64(a.n))
			a.ready = true
		}
	} else {
		// ADX Wilder smoothing: (prevADX*(N-1) + DX) / N
		a.adx = roundDivPositive(a.adx*int64(a.n-1)+dxVal, int64(a.n))
	}

	a.prev = c
}

// Optional: expose DI values if you want them in strategies/debugging.
func (a *ADX) PlusDI() float64  { return fixedScaledToFloat64(a.plusDI) }
func (a *ADX) MinusDI() float64 { return fixedScaledToFloat64(a.minusDI) }
func (a *ADX) DX() float64      { return fixedScaledToFloat64(a.lastDX) }

func diScaled(smPlusDM, smMinusDM, smTR int64) (plusDI, minusDI int64) {
	if smTR <= 0 {
		return 0, 0
	}
	plusDI = percentScaled(smPlusDM, smTR)
	minusDI = percentScaled(smMinusDM, smTR)
	return plusDI, minusDI
}

func dxScaled(plusDI, minusDI int64) int64 {
	den := plusDI + minusDI
	if den <= 0 {
		return 0
	}
	diff := plusDI - minusDI
	if diff < 0 {
		diff = -diff
	}
	return percentScaled(diff, den)
}

var _ Float64Indicator = (*ADX)(nil)
