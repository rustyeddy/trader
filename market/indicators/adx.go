package indicators

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/market"
)

// ADX computes the Average Directional Index (Wilder) over candle OHLC.
//
// Pricing note:
// - market.Candle prices are scaled integers.
// - ADX outputs float64 (0..100-ish) and uses float math internally.
// - Pass the same scale used to build your CandleSet (e.g. 1_000_000 for Dukascopy).
//
// Readiness / warmup:
// - ADX needs:
//  1. N periods to build initial smoothed TR/+DM/-DM
//  2. N DX values to seed the initial ADX (average of first N DX)
//
// - Practically, that's about 2N "periods" (differences between candles), plus the first candle.
// - We expose Warmup() as 2N to keep it simple/consistent with your other indicators.
type ADX struct {
	n     int
	scale float64
	name  string

	// candle tracking
	seen    int
	prev    market.OHLC
	hasPrev bool
	ready   bool
	adx     float64
	plusDI  float64
	minusDI float64
	lastDX  float64
	periods int // number of computed periods (needs prev)

	// initial accumulation for first N periods
	sumTR      float64
	sumPlusDM  float64
	sumMinusDM float64

	// Wilder smoothed values after initialization
	smTR      float64
	smPlusDM  float64
	smMinusDM float64

	// seeding ADX: average of first N DX values
	dxSum   float64
	dxCount int
}

func NewADX(period int, scale int32) *ADX {
	if period <= 0 {
		panic("ADX period must be > 0")
	}
	if scale <= 0 {
		panic("ADX scale must be > 0")
	}
	return &ADX{
		n:     period,
		scale: float64(scale),
		name:  fmt.Sprintf("ADX(%d)", period),
	}
}

func (a *ADX) Name() string     { return a.name }
func (a *ADX) Warmup() int      { return 2 * a.n }
func (a *ADX) Ready() bool      { return a.ready }
func (a *ADX) Float64() float64 { return a.adx }

func (a *ADX) Reset() {
	*a = ADX{
		n:     a.n,
		scale: a.scale,
		name:  a.name,
	}
}

// Update consumes the next closed candle.
func (a *ADX) Update(c market.OHLC) {
	a.seen++

	// Need a previous candle to form a "period"
	if !a.hasPrev {
		a.prev = c
		a.hasPrev = true
		return
	}

	// Convert scaled ints -> float price units
	prevH := float64(a.prev.H) / a.scale
	prevL := float64(a.prev.L) / a.scale
	prevC := float64(a.prev.C) / a.scale

	h := float64(c.H) / a.scale
	l := float64(c.L) / a.scale

	// True Range (Wilder)
	tr := max3(h-l, math.Abs(h-prevC), math.Abs(l-prevC))

	// Directional Movement
	upMove := h - prevH
	downMove := prevL - l

	var plusDM, minusDM float64
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
		a.sumTR += tr
		a.sumPlusDM += plusDM
		a.sumMinusDM += minusDM

		// When we have N periods accumulated, initialize smoothed values
		if a.periods == a.n {
			a.smTR = a.sumTR
			a.smPlusDM = a.sumPlusDM
			a.smMinusDM = a.sumMinusDM

			a.plusDI, a.minusDI = di(a.smPlusDM, a.smMinusDM, a.smTR)
			dx := dx(a.plusDI, a.minusDI)
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
	nf := float64(a.n)
	a.smTR = a.smTR - (a.smTR / nf) + tr
	a.smPlusDM = a.smPlusDM - (a.smPlusDM / nf) + plusDM
	a.smMinusDM = a.smMinusDM - (a.smMinusDM / nf) + minusDM

	a.plusDI, a.minusDI = di(a.smPlusDM, a.smMinusDM, a.smTR)
	dxVal := dx(a.plusDI, a.minusDI)
	a.lastDX = dxVal

	// Seed ADX using the first N DX values, then Wilder-smooth ADX
	if !a.ready {
		a.dxSum += dxVal
		a.dxCount++
		if a.dxCount >= a.n {
			a.adx = a.dxSum / nf
			a.ready = true
		}
	} else {
		// ADX Wilder smoothing: (prevADX*(N-1) + DX) / N
		a.adx = (a.adx*(nf-1.0) + dxVal) / nf
	}

	a.prev = c
}

// Optional: expose DI values if you want them in strategies/debugging.
func (a *ADX) PlusDI() float64  { return a.plusDI }
func (a *ADX) MinusDI() float64 { return a.minusDI }
func (a *ADX) DX() float64      { return a.lastDX }

func di(smPlusDM, smMinusDM, smTR float64) (plusDI, minusDI float64) {
	if smTR <= 0 {
		return 0, 0
	}
	plusDI = 100.0 * (smPlusDM / smTR)
	minusDI = 100.0 * (smMinusDM / smTR)
	return plusDI, minusDI
}

func dx(plusDI, minusDI float64) float64 {
	den := plusDI + minusDI
	if den <= 0 {
		return 0
	}
	return 100.0 * (math.Abs(plusDI-minusDI) / den)
}

func max3(a, b, c float64) float64 {
	if a >= b && a >= c {
		return a
	}
	if b >= a && b >= c {
		return b
	}
	return c
}
