package trader

import (
	"fmt"
	"math"
)

// BollingerBands computes Bollinger Bands over candle closes.
// Middle = SMA(n), Upper = Middle + k×σ, Lower = Middle − k×σ
// where σ is the population standard deviation of the last n closes.
type BollingerBands struct {
	n       int
	k       float64
	kScaled int64
	scale   Scale6

	closes []Price
	pos    int
	count  int

	sum        PriceSum
	sumSquares int64
	middle     Price
	upper      Price
	lower      Price
	stdDev     Price
}

func NewBollingerBands(period int, multiplier float64, scale Scale6) (*BollingerBands, error) {
	if period < 2 {
		return nil, fmt.Errorf("BollingerBands: period must be >= 2")
	}
	if multiplier <= 0 {
		return nil, fmt.Errorf("BollingerBands: multiplier must be > 0")
	}
	if scale <= 0 {
		return nil, fmt.Errorf("BollingerBands: scale must be > 0")
	}
	return &BollingerBands{
		n:       period,
		k:       multiplier,
		kScaled: int64(math.Round(multiplier * float64(indicatorValueScale))),
		scale:   scale,
		closes:  make([]Price, period),
	}, nil
}

func (b *BollingerBands) Name() string { return fmt.Sprintf("BB(%d,%.1f)", b.n, b.k) }
func (b *BollingerBands) Period() int  { return b.n }
func (b *BollingerBands) Warmup() int  { return b.n }
func (b *BollingerBands) Ready() bool  { return b.count >= b.n }

func (b *BollingerBands) Middle() float64 { return priceToFloat64(int64(b.middle), b.scale) }
func (b *BollingerBands) Upper() float64  { return priceToFloat64(int64(b.upper), b.scale) }
func (b *BollingerBands) Lower() float64  { return priceToFloat64(int64(b.lower), b.scale) }
func (b *BollingerBands) StdDev() float64 { return priceToFloat64(int64(b.stdDev), b.scale) }

func (b *BollingerBands) MiddlePrice() Price { return b.middle }
func (b *BollingerBands) UpperPrice() Price  { return b.upper }
func (b *BollingerBands) LowerPrice() Price  { return b.lower }
func (b *BollingerBands) StdDevPrice() Price { return b.stdDev }

// PercentB returns where price sits relative to the bands: 0.0 = lower, 1.0 = upper, 0.5 = middle.
func (b *BollingerBands) PercentB(price float64) float64 {
	return b.PercentBPrice(Price(math.Round(price * float64(b.scale))))
}

func (b *BollingerBands) PercentBPrice(price Price) float64 {
	width := int64(b.upper - b.lower)
	if width == 0 {
		return 0.5
	}
	return fixedScaledToFloat64(roundDivSigned(int64(price-b.lower)*indicatorValueScale, width))
}

// BandWidth returns (upper − lower) / middle — a normalised squeeze measure.
func (b *BollingerBands) BandWidth() float64 {
	if b.middle == 0 {
		return 0
	}
	return fixedScaledToFloat64(roundDivPositive(int64(b.upper-b.lower)*indicatorValueScale, int64(b.middle)))
}

func (b *BollingerBands) Update(c Candle) {
	if b.count == b.n {
		evicted := b.closes[b.pos]
		b.sum -= PriceSum(evicted)
		b.sumSquares -= int64(evicted) * int64(evicted)
	} else {
		b.count++
	}

	b.closes[b.pos] = c.Close
	b.sum += PriceSum(c.Close)
	b.sumSquares += int64(c.Close) * int64(c.Close)
	b.pos = (b.pos + 1) % b.n
	if b.count < b.n {
		return
	}

	n64 := int64(b.n)
	sum := int64(b.sum)
	b.middle = Price(roundDivPositive(sum, n64))

	varianceNum := n64*b.sumSquares - sum*sum
	if varianceNum < 0 {
		varianceNum = 0
	}
	b.stdDev = Price(sqrtRoundRatio(varianceNum, n64))

	offset := Price(roundDivPositive(int64(b.stdDev)*b.kScaled, indicatorValueScale))
	b.upper = b.middle + offset
	b.lower = b.middle - offset
}

func (b *BollingerBands) Reset() {
	for i := range b.closes {
		b.closes[i] = 0
	}
	b.pos = 0
	b.count = 0
	b.sum = 0
	b.sumSquares = 0
	b.middle = 0
	b.upper = 0
	b.lower = 0
	b.stdDev = 0
}

var _ CandleIndicator = (*BollingerBands)(nil)
