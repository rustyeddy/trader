package indicators

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/pricing"
)

// ADX implements Wilder's Average Directional Index (trend strength).
//
// Output is the conventional 0..100 ADX value (float64), independent of the
// candle price scale.
//
// Warmup:
//  - Need 1 candle to seed prev
//  - Need Period TR/+DM/-DM samples to seed Wilder smoothing
//  - Need Period DX samples to seed ADX
// Total warmup: 2*Period + 1
//
// References: J. Welles Wilder, "New Concepts in Technical Trading Systems".
type ADX struct {
	period int

	prev     pricing.Candle
	havePrev bool

	// initial sums for seeding Wilder smoothing
	initCount int
	sumTR     float64
	sumPDM    float64
	sumMDM    float64

	// Wilder-smoothed values
	trN  float64
	pdmN float64
	mdmN float64

	// ADX seeding
	dxCount int
	dxSum   float64

	adx   float64
	ready bool
}

func NewADX(period int) *ADX {
	return &ADX{period: period}
}

func (a *ADX) Name() string { return fmt.Sprintf("ADX(%d)", a.period) }

func (a *ADX) Warmup() int { return 2*a.period + 1 }

func (a *ADX) Reset() {
	*a = ADX{period: a.period}
}

func (a *ADX) Ready() bool { return a.ready }

func (a *ADX) Value() float64 {
	if !a.ready {
		return 0
	}
	return a.adx
}

func (a *ADX) Update(c pricing.Candle) {
	if !a.havePrev {
		a.prev = c
		a.havePrev = true
		return
	}

	// Directional Movement
	upMove := float64(c.H - a.prev.H)
	downMove := float64(a.prev.L - c.L)

	pdm := 0.0
	mdm := 0.0
	if upMove > downMove && upMove > 0 {
		pdm = upMove
	}
	if downMove > upMove && downMove > 0 {
		mdm = downMove
	}

	// True Range
	tr := trueRangeF(c, a.prev)

	// advance prev
	a.prev = c

	p := float64(a.period)

	// Phase A: seed Wilder smoothing with average of first Period TR/DM samples
	if a.initCount < a.period {
		a.sumTR += tr
		a.sumPDM += pdm
		a.sumMDM += mdm
		a.initCount++

		if a.initCount == a.period {
			a.trN = a.sumTR / p
			a.pdmN = a.sumPDM / p
			a.mdmN = a.sumMDM / p
		}
		return
	}

	// Wilder smoothing
	a.trN = (a.trN*(p-1) + tr) / p
	a.pdmN = (a.pdmN*(p-1) + pdm) / p
	a.mdmN = (a.mdmN*(p-1) + mdm) / p

	if a.trN <= 0 {
		return
	}

	pdi := 100.0 * (a.pdmN / a.trN)
	mdi := 100.0 * (a.mdmN / a.trN)
	den := pdi + mdi
	if den == 0 {
		return
	}

	dx := 100.0 * math.Abs(pdi-mdi) / den

	// Phase B: seed ADX with average of first Period DX values
	if !a.ready {
		a.dxSum += dx
		a.dxCount++
		if a.dxCount == a.period {
			a.adx = a.dxSum / p
			a.ready = true
		}
		return
	}

	// Wilder smoothing for ADX
	a.adx = (a.adx*(p-1) + dx) / p
}

func trueRangeF(cur, prev pricing.Candle) float64 {
	highLow := float64(cur.H - cur.L)
	highClose := math.Abs(float64(cur.H - prev.C))
	lowClose := math.Abs(float64(cur.L - prev.C))
	return math.Max(highLow, math.Max(highClose, lowClose))
}
