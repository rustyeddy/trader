package trader

import (
	"context"
	"sort"
)

// Stat is a single labeled measurement returned by an Analyzer.
type Stat struct {
	Name  string
	Value string
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
func RunAnalysis(ctx context.Context, itr CandleIterator, analyzers []Analyzer) error {
	defer itr.Close()
	for itr.Next() {
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

// sortedCopy returns a sorted copy of vals.
func sortedCopy(vals []float64) []float64 {
	cp := make([]float64, len(vals))
	copy(cp, vals)
	sort.Float64s(cp)
	return cp
}

// percentile returns the p-th percentile (0–100) of a pre-sorted slice using
// linear interpolation.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	idx := p / 100.0 * float64(n-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= n {
		return sorted[n-1]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}
