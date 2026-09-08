package analysis

import (
	"math"
	"math/rand/v2"
	"sort"
)

// BootstrapConfig controls the nonparametric bootstrap confidence
// interval RunRSIRegimeEventStudy computes for each cell's mean forward
// return, per docs/research/eqr-01-research-protocol.org's predeclared
// default method: a nonparametric bootstrap of the mean, 2,000
// resamples with replacement, 95% percentile interval.
//
// Bootstrap resampling is the one source of randomness this package
// permits (issue #319's own "no hidden random behavior except
// bootstrap resampling, whose seed must be explicit/fixed and recorded"
// requirement). Seed1/Seed2 must always be supplied explicitly by the
// caller and recorded as run provenance — there is no default seed
// baked into this package, so two independent runs cannot silently
// diverge, or silently collide, by omission.
type BootstrapConfig struct {
	// Resamples is the number of bootstrap resamples to draw. The
	// protocol's default is 2,000. Must be positive.
	Resamples int
	// Seed1 and Seed2 seed a math/rand/v2 PCG source
	// (rand.NewPCG(Seed1, Seed2)), matching this codebase's existing
	// deterministic-PRNG convention (id/deterministic.go). Two
	// independent runs constructed with the same Seed1/Seed2, given the
	// same input returns, produce bit-identical bootstrap results.
	Seed1, Seed2 uint64
}

func (c BootstrapConfig) validate() error {
	if c.Resamples <= 0 {
		return ErrInvalidBootstrapResamples
	}
	return nil
}

// bootstrapMeanCI computes a 95% percentile bootstrap confidence
// interval for the mean of returns: it draws cfg.Resamples resamples,
// each the same size as returns and sampled with replacement, from a
// PCG source seeded by cfg.Seed1/cfg.Seed2, and returns the 2.5th and
// 97.5th percentiles of the resulting distribution of resample means.
//
// It returns (0, 0) for an empty input, where no interval can be
// constructed, and does not itself apply any minimum-observation-count
// policy — that is RunRSIRegimeEventStudy's responsibility (see
// RSICellStats.Insufficient).
func bootstrapMeanCI(returns []float64, cfg BootstrapConfig) (lower, upper float64) {
	n := len(returns)
	if n == 0 {
		return 0, 0
	}

	rng := rand.New(rand.NewPCG(cfg.Seed1, cfg.Seed2))
	means := make([]float64, cfg.Resamples)
	for i := range cfg.Resamples {
		var sum float64
		for range n {
			sum += returns[rng.IntN(n)]
		}
		means[i] = sum / float64(n)
	}
	sort.Float64s(means)

	lo := percentileIndex(len(means), 0.025)
	hi := percentileIndex(len(means), 0.975)
	return means[lo], means[hi]
}

// percentileIndex returns the index into a length-n sorted slice
// nearest the p-th percentile (0 <= p <= 1), via the nearest-rank
// method rounded to the closest index — a small, fixed, documented
// convention rather than a full interpolating-percentile
// implementation, adequate for this package's fixed 2.5/97.5 use.
func percentileIndex(n int, p float64) int {
	idx := int(math.Round(p * float64(n-1)))
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return idx
}
