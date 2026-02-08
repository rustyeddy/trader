package indicators

import (
	"math"

	"github.com/rustyeddy/trader/pricing"
)

// ADX implements Wilder's Average Directional Index (trend strength).
// Usage:
//
//	adx := indicators.NewADX(14)
//	val, ok := adx.Update(candle)
//	if ok && val >= 20 { ... }
type ADX struct {
	Period int

	prev     pricing.Candle
	havePrev bool

	// Wilder-smoothed values after warmup:
	tr14  float64
	pdm14 float64
	mdm14 float64

	adx   float64
	dxSum float64

	// count of candles processed (including the first prev seed)
	count int
	ready bool
}

func NewADX(period int) *ADX {
	return &ADX{Period: period}
}

func (a *ADX) Value() float64 {
	return a.adx
}

func (a *ADX) Ready() bool {
	return a.ready
}

// Update consumes the next candle and returns (adx, ready).
// ready becomes true after enough candles to compute a stable ADX:
// - Need Period candles to initialize smoothed TR/+DM/-DM
// - Then Period DX values to initialize ADX
// Total: 2*Period candles after the initial prev seed.
func (a *ADX) Update(c pricing.Candle) (float64, bool) {
	// Seed previous candle
	if !a.havePrev {
		a.prev = c
		a.havePrev = true
		a.count = 1
		return 0, false
	}

	// 1) Compute directional movement using current vs previous highs/lows
	upMove := c.High - a.prev.High
	downMove := a.prev.Low - c.Low

	var pdm, mdm float64
	if upMove > downMove && upMove > 0 {
		pdm = upMove
	}
	if downMove > upMove && downMove > 0 {
		mdm = downMove
	}

	// 2) True Range (TR)
	// tr := trueRange(a.prev.Close, c.High, c.Low)
	tr := trueRange(c, a.prev)

	a.prev = c
	a.count++

	// Warmup Phase A: accumulate initial averages up to Period
	// We start collecting on the second candle, so "samples" for TR/DM begin at count=2.
	if a.count <= a.Period+1 {
		a.tr14 += tr
		a.pdm14 += pdm
		a.mdm14 += mdm

		// When we have Period samples of TR/DM (i.e. count == Period+1),
		// convert sums to simple averages to seed Wilder smoothing.
		if a.count == a.Period+1 {
			p := float64(a.Period)
			a.tr14 /= p
			a.pdm14 /= p
			a.mdm14 /= p
		}
		return 0, false
	}

	// 3) Wilder smoothing for TR/+DM/-DM
	p := float64(a.Period)
	a.tr14 = (a.tr14*(p-1) + tr) / p
	a.pdm14 = (a.pdm14*(p-1) + pdm) / p
	a.mdm14 = (a.mdm14*(p-1) + mdm) / p

	// Guard: avoid divide-by-zero if data is pathological
	if a.tr14 == 0 {
		return 0, false
	}

	// 4) DI and DX
	pdi := 100.0 * (a.pdm14 / a.tr14)
	mdi := 100.0 * (a.mdm14 / a.tr14)
	den := pdi + mdi
	if den == 0 {
		return 0, false
	}

	dx := 100.0 * math.Abs(pdi-mdi) / den

	// Warmup Phase B: seed ADX with average of first Period DX values.
	// We begin producing DX after count > Period+1.
	//
	// First DX occurs at count == Period+2.
	// After collecting Period DX values (count == 2*Period+1), we seed ADX.
	firstDXCount := a.Period + 2
	seedADXCount := 2*a.Period + 1

	if !a.ready {
		// accumulate DX for seeding
		if a.count >= firstDXCount && a.count <= seedADXCount {
			a.dxSum += dx
		}
		if a.count == seedADXCount {
			a.adx = a.dxSum / p
			a.ready = true
			return a.adx, true
		}
		return 0, false
	}

	// 5) Wilder smoothing for ADX
	a.adx = (a.adx*(p-1) + dx) / p
	return a.adx, true
}
