package trader

import (
	"fmt"
)

// ChoppinessIndex measures whether price action is trending or ranging.
//
// Formula: 100 × log10(Σ TR(1,N) / (HH(N) − LL(N))) / log10(N)
//
// Values near 100 = choppy/consolidating; near 0 = strongly trending.
// Conventional threshold: 61.8 (trending below, ranging above).
type ChoppinessIndex struct {
	n         int
	name      string
	logPeriod int64

	prevClose Price
	hasPrev   bool
	ready     bool
	value     int64

	buf   []ciBar
	pos   int
	count int
	sumTR PriceSum
}

type ciBar struct {
	tr   PriceSum
	high Price
	low  Price
}

func NewChoppinessIndex(period int, scale Scale6) (*ChoppinessIndex, error) {
	if period < 2 {
		return nil, fmt.Errorf("ChoppinessIndex period must be >= 2")
	}
	if scale <= 0 {
		return nil, fmt.Errorf("ChoppinessIndex scale must be > 0")
	}
	logPeriod := fixedLog10Scaled(int64(period)*indicatorValueScale, indicatorValueScale)
	return &ChoppinessIndex{
		n:         period,
		name:      fmt.Sprintf("CI(%d)", period),
		logPeriod: logPeriod,
		buf:       make([]ciBar, period),
	}, nil
}

func (c *ChoppinessIndex) Name() string     { return c.name }
func (c *ChoppinessIndex) Period() int      { return c.n }
func (c *ChoppinessIndex) Ready() bool      { return c.ready }
func (c *ChoppinessIndex) Float64() float64 { return fixedScaledToFloat64(c.value) }
func (c *ChoppinessIndex) Warmup() int      { return c.n }

func (c *ChoppinessIndex) Reset() {
	*c = ChoppinessIndex{
		n:         c.n,
		name:      c.name,
		logPeriod: c.logPeriod,
		buf:       make([]ciBar, c.n),
	}
}

func (c *ChoppinessIndex) Update(candle Candle) {
	h := candle.High
	l := candle.Low
	cl := candle.Close

	tr := int64(h - l)
	if c.hasPrev {
		tr = max3Int64(int64(h-l), absPriceDiff(h, c.prevClose), absPriceDiff(l, c.prevClose))
	}
	c.prevClose = cl
	c.hasPrev = true

	// Evict the oldest bar's TR from the running sum when the buffer is full.
	if c.count == c.n {
		c.sumTR -= c.buf[c.pos].tr
	} else {
		c.count++
	}
	c.buf[c.pos] = ciBar{tr: PriceSum(tr), high: h, low: l}
	c.pos = (c.pos + 1) % c.n
	c.sumTR += PriceSum(tr)

	if c.count < c.n {
		return
	}

	// Scan the full window for highest high and lowest low.
	hh, ll := c.buf[0].high, c.buf[0].low
	for _, b := range c.buf {
		if b.high > hh {
			hh = b.high
		}
		if b.low < ll {
			ll = b.low
		}
	}

	rangeHL := int64(hh - ll)
	if rangeHL <= 0 || c.sumTR <= 0 {
		c.value = 100 * indicatorValueScale
		c.ready = true
		return
	}

	ratioScaled := roundDivPositive(int64(c.sumTR)*indicatorValueScale, rangeHL)
	logRatio := fixedLog10Scaled(ratioScaled, indicatorValueScale)
	c.value = roundDivSigned(logRatio*indicatorValueScale, c.logPeriod) * 100
	c.ready = true
}

var _ Float64Indicator = (*ChoppinessIndex)(nil)
