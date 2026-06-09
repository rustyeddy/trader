package trader

import (
	"context"
	"sort"
)

// Stat is a single labeled measurement returned by an Analyzer.
// Pips is the raw pip count when Value is a pip measurement; zero otherwise.
// Callers can use Pips to convert to a currency amount without re-parsing Value.
type Stat struct {
	Name  string
	Value string
	Pips  float64
}

// Analyzer accumulates statistics over a candle sequence.
type Analyzer interface {
	Name() string
	Update(*CandleTime)
	Stats() []Stat
}

// CandleIterator is the read-only traversal interface exposed to callers
// outside this package (e.g. cmd/data).  The unexported candleIterator is a
// superset of this interface, so all existing implementations satisfy it.
type CandleIterator interface {
	Next() bool
	CandleTime() CandleTime
	Err() error
	Close() error
}

// RunAnalysis walks itr, feeding every candle to each Analyzer.
// It closes itr before returning.
func RunAnalysis(ctx context.Context, itr CandleIterator, analyzers []Analyzer) (err error) {
	defer func() {
		if closeErr := itr.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !itr.Next() {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		ct := itr.CandleTime()
		for _, a := range analyzers {
			a.Update(&ct)
		}
	}
	return itr.Err()
}

func validOHLC(c Candle) bool {
	return c.High > c.Low &&
		c.Open >= c.Low && c.Open <= c.High &&
		c.Close >= c.Low && c.Close <= c.High
}

func missingInstrumentStats() []Stat {
	return []Stat{{Name: "error", Value: "missing instrument"}}
}

type priceDistribution struct {
	counts map[Price]int
	count  int
	sum    int64
	min    Price
	max    Price
}

func (d *priceDistribution) Add(v Price) {
	if d.counts == nil {
		d.counts = make(map[Price]int)
		d.min = v
		d.max = v
	}
	if v < d.min {
		d.min = v
	}
	if v > d.max {
		d.max = v
	}
	d.counts[v]++
	d.count++
	d.sum += int64(v)
}

func (d *priceDistribution) Len() int {
	return d.count
}

func (d *priceDistribution) MeanPips(uPip float64) float64 {
	if d.count == 0 {
		return 0
	}
	return float64(d.sum) / float64(d.count) / uPip
}

func (d *priceDistribution) MinPips(uPip float64) float64 {
	if d.count == 0 {
		return 0
	}
	return float64(d.min) / uPip
}

func (d *priceDistribution) MaxPips(uPip float64) float64 {
	if d.count == 0 {
		return 0
	}
	return float64(d.max) / uPip
}

func (d *priceDistribution) PercentilePips(p float64, uPip float64) float64 {
	n := d.count
	if n == 0 {
		return 0
	}
	if n == 1 {
		return d.MinPips(uPip)
	}
	p = clampPercentile(p)
	idx := p / 100.0 * float64(n-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= n {
		return d.MaxPips(uPip)
	}
	frac := idx - float64(lo)
	loVal := float64(d.valueAt(lo)) / uPip
	hiVal := float64(d.valueAt(hi)) / uPip
	return loVal*(1-frac) + hiVal*frac
}

func (d *priceDistribution) valueAt(idx int) Price {
	seen := 0
	for _, v := range d.sortedPrices() {
		seen += d.counts[v]
		if idx < seen {
			return v
		}
	}
	return d.max
}

func (d *priceDistribution) sortedPrices() []Price {
	vals := make([]Price, 0, len(d.counts))
	for v := range d.counts {
		vals = append(vals, v)
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	return vals
}

// unitsPerPip returns the number of Price units that equal one pip for inst.
func unitsPerPip(inst *Instrument) float64 {
	return float64(PriceScale) * inst.PipSize()
}

// pricesToPips converts a slice of Price deltas to pip values.
func pricesToPips(prices []Price, uPip float64) []float64 {
	out := make([]float64, len(prices))
	for i, p := range prices {
		out[i] = float64(p) / uPip
	}
	return out
}

// sortedCopy returns a sorted copy of vals.
func sortedCopy(vals []float64) []float64 {
	cp := make([]float64, len(vals))
	copy(cp, vals)
	sort.Float64s(cp)
	return cp
}

func clampPercentile(p float64) float64 {
	switch {
	case p < 0:
		return 0
	case p > 100:
		return 100
	default:
		return p
	}
}

// percentile returns the p-th percentile of a pre-sorted slice using linear
// interpolation. Values outside the 0-100 percentile range are clamped.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	p = clampPercentile(p)
	idx := p / 100.0 * float64(n-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= n {
		return sorted[n-1]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}
