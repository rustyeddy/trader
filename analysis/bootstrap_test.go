package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBootstrapMeanCI_DeterministicForFixedSeed proves the "no hidden
// random behavior except bootstrap resampling, whose seed must be
// explicit/fixed and recorded" requirement (issue #319): two calls with
// identical returns and an identical BootstrapConfig produce
// bit-identical results.
func TestBootstrapMeanCI_DeterministicForFixedSeed(t *testing.T) {
	returns := []float64{0.01, -0.02, 0.03, 0.015, -0.01, 0.02, -0.005, 0.008}
	cfg := BootstrapConfig{Resamples: 500, Seed1: 42, Seed2: 7}

	lower1, upper1 := bootstrapMeanCI(returns, cfg)
	lower2, upper2 := bootstrapMeanCI(returns, cfg)

	assert.Equal(t, lower1, lower2)
	assert.Equal(t, upper1, upper2)
	assert.LessOrEqual(t, lower1, upper1)
}

// TestBootstrapMeanCI_DifferentSeedsCanDiffer is a sanity check that the
// seed genuinely drives the resampling (not a fixed no-op): a
// differently seeded run over the same returns produces a different
// interval, at least for one of the two bounds, for this fixed input.
func TestBootstrapMeanCI_DifferentSeedsCanDiffer(t *testing.T) {
	returns := []float64{0.01, -0.02, 0.03, 0.015, -0.01, 0.02, -0.005, 0.008, 0.04, -0.03}

	lower1, upper1 := bootstrapMeanCI(returns, BootstrapConfig{Resamples: 500, Seed1: 1, Seed2: 1})
	lower2, upper2 := bootstrapMeanCI(returns, BootstrapConfig{Resamples: 500, Seed1: 99, Seed2: 12345})

	assert.False(t, lower1 == lower2 && upper1 == upper2,
		"expected different seeds to produce a different bootstrap interval")
}

// TestBootstrapMeanCI_IntervalWithinObservedRange proves the bootstrap
// mean CI never escapes the min/max of the observed returns — a basic
// sanity property of resampling with replacement from a fixed sample.
func TestBootstrapMeanCI_IntervalWithinObservedRange(t *testing.T) {
	returns := []float64{-0.05, -0.01, 0.0, 0.02, 0.1}
	lower, upper := bootstrapMeanCI(returns, BootstrapConfig{Resamples: 2000, Seed1: 5, Seed2: 5})

	assert.GreaterOrEqual(t, lower, -0.05)
	assert.LessOrEqual(t, upper, 0.1)
}

func TestBootstrapMeanCI_EmptyReturnsIsZero(t *testing.T) {
	lower, upper := bootstrapMeanCI(nil, BootstrapConfig{Resamples: 2000, Seed1: 1, Seed2: 1})
	assert.Zero(t, lower)
	assert.Zero(t, upper)
}

func TestBootstrapConfig_Validate(t *testing.T) {
	assert.ErrorIs(t, BootstrapConfig{Resamples: 0}.validate(), ErrInvalidBootstrapResamples)
	assert.NoError(t, BootstrapConfig{Resamples: 1}.validate())
}
