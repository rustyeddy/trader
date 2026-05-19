package trader

import (
	"fmt"
	"math"
)

// ChoppinessIndex measures whether price action is trending or ranging.
//
// Formula: 100 × log10(Σ TR(1,N) / (HH(N) − LL(N))) / log10(N)
//
// Values near 100 = choppy/consolidating; near 0 = strongly trending.
// Conventional threshold: 61.8 (trending below, ranging above).
type ChoppinessIndex struct {
	n     int
	scale float64
	name  string

	prevClose float64
	hasPrev   bool
	ready     bool
	value     float64

	buf   []ciBar
	pos   int
	count int
	sumTR float64
}

type ciBar struct {
	tr   float64
	high float64
	low  float64
}

func NewChoppinessIndex(period int, scale Scale6) *ChoppinessIndex {
	if period < 2 {
		panic("ChoppinessIndex period must be >= 2")
	}
	if scale <= 0 {
		panic("ChoppinessIndex scale must be > 0")
	}
	return &ChoppinessIndex{
		n:     period,
		scale: float64(scale),
		name:  fmt.Sprintf("CI(%d)", period),
		buf:   make([]ciBar, period),
	}
}

func (c *ChoppinessIndex) Name() string   { return c.name }
func (c *ChoppinessIndex) Ready() bool    { return c.ready }
func (c *ChoppinessIndex) Value() float64 { return c.value }
func (c *ChoppinessIndex) Warmup() int    { return c.n }

func (c *ChoppinessIndex) Reset() {
	*c = ChoppinessIndex{
		n:     c.n,
		scale: c.scale,
		name:  c.name,
		buf:   make([]ciBar, c.n),
	}
}

func (c *ChoppinessIndex) Update(candle Candle) {
	h := float64(candle.High) / c.scale
	l := float64(candle.Low) / c.scale
	cl := float64(candle.Close) / c.scale

	tr := h - l
	if c.hasPrev {
		tr = max3(h-l, math.Abs(h-c.prevClose), math.Abs(l-c.prevClose))
	}
	c.prevClose = cl
	c.hasPrev = true

	// Evict the oldest bar's TR from the running sum when the buffer is full.
	if c.count == c.n {
		c.sumTR -= c.buf[c.pos].tr
	} else {
		c.count++
	}
	c.buf[c.pos] = ciBar{tr: tr, high: h, low: l}
	c.pos = (c.pos + 1) % c.n
	c.sumTR += tr

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

	rangeHL := hh - ll
	if rangeHL <= 0 || c.sumTR <= 0 {
		c.value = 100
		c.ready = true
		return
	}

	c.value = 100 * math.Log10(c.sumTR/rangeHL) / math.Log10(float64(c.n))
	c.ready = true
}
