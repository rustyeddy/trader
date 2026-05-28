package trader

import (
	"fmt"
	"math"
)

// BollingerBands computes Bollinger Bands over candle closes.
// Middle = SMA(n), Upper = Middle + k×σ, Lower = Middle − k×σ
// where σ is the population standard deviation of the last n closes.
type BollingerBands struct {
	n     int
	k     float64
	scale float64

	closes []float64 // ring buffer of closes in price units (unscaled)
	pos    int
	count  int

	middle float64
	upper  float64
	lower  float64
	stdDev float64
}

func NewBollingerBands(period int, multiplier float64, scale Scale6) *BollingerBands {
	if period < 2 {
		panic("BollingerBands: period must be >= 2")
	}
	if multiplier <= 0 {
		panic("BollingerBands: multiplier must be > 0")
	}
	if scale <= 0 {
		panic("BollingerBands: scale must be > 0")
	}
	return &BollingerBands{
		n:      period,
		k:      multiplier,
		scale:  float64(scale),
		closes: make([]float64, period),
	}
}

func (b *BollingerBands) Name() string { return fmt.Sprintf("BB(%d,%.1f)", b.n, b.k) }
func (b *BollingerBands) Period() int  { return b.n }
func (b *BollingerBands) Ready() bool  { return b.count >= b.n }

func (b *BollingerBands) Middle() float64 { return b.middle }
func (b *BollingerBands) Upper() float64  { return b.upper }
func (b *BollingerBands) Lower() float64  { return b.lower }
func (b *BollingerBands) StdDev() float64 { return b.stdDev }

func (b *BollingerBands) MiddlePrice() Price { return Price(math.Round(b.middle * b.scale)) }
func (b *BollingerBands) UpperPrice() Price  { return Price(math.Round(b.upper * b.scale)) }
func (b *BollingerBands) LowerPrice() Price  { return Price(math.Round(b.lower * b.scale)) }

// PercentB returns where price sits relative to the bands: 0.0 = lower, 1.0 = upper, 0.5 = middle.
func (b *BollingerBands) PercentB(price float64) float64 {
	width := b.upper - b.lower
	if width == 0 {
		return 0.5
	}
	return (price - b.lower) / width
}

// BandWidth returns (upper − lower) / middle — a normalised squeeze measure.
func (b *BollingerBands) BandWidth() float64 {
	if b.middle == 0 {
		return 0
	}
	return (b.upper - b.lower) / b.middle
}

func (b *BollingerBands) Update(c Candle) {
	x := float64(c.Close) / b.scale
	b.closes[b.pos] = x
	b.pos = (b.pos + 1) % b.n
	if b.count < b.n {
		b.count++
	}
	if b.count < b.n {
		return
	}
	var sum float64
	for _, v := range b.closes {
		sum += v
	}
	mean := sum / float64(b.n)
	var variance float64
	for _, v := range b.closes {
		d := v - mean
		variance += d * d
	}
	variance /= float64(b.n)
	sd := math.Sqrt(variance)
	b.middle = mean
	b.stdDev = sd
	b.upper = mean + b.k*sd
	b.lower = mean - b.k*sd
}

func (b *BollingerBands) Reset() {
	for i := range b.closes {
		b.closes[i] = 0
	}
	b.pos = 0
	b.count = 0
	b.middle = 0
	b.upper = 0
	b.lower = 0
	b.stdDev = 0
}
