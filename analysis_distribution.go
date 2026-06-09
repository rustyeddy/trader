package trader

import "sort"

type priceDistribution struct {
	counts map[Price]int
	count  int
	sum    PriceSum
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
	d.sum += PriceSum(v)
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
	return d.percentilePips(p, uPip, d.sortedPrices())
}

func (d *priceDistribution) percentilePips(p float64, uPip float64, sorted []Price) float64 {
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
	loVal := float64(d.valueAt(lo, sorted)) / uPip
	hiVal := float64(d.valueAt(hi, sorted)) / uPip
	return loVal*(1-frac) + hiVal*frac
}

func (d *priceDistribution) valueAt(idx int, sorted []Price) Price {
	seen := 0
	for _, v := range sorted {
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
