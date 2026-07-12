package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeThresholds_ZeroOverrideKeepsBase(t *testing.T) {
	base := DefaultThresholds()
	merged := MergeThresholds(base, Thresholds{})
	assert.Equal(t, base, merged)
}

func TestMergeThresholds_NonZeroOverrideWins(t *testing.T) {
	base := DefaultThresholds()
	override := Thresholds{H4ADXFloor: 15.0, ValueZoneMax: 2.0}

	merged := MergeThresholds(base, override)

	assert.Equal(t, 15.0, merged.H4ADXFloor)
	assert.Equal(t, 2.0, merged.ValueZoneMax)
	// Everything else falls back to base (the default).
	assert.Equal(t, base.HotD1ADXFloor, merged.HotD1ADXFloor)
	assert.Equal(t, base.H4MinEMASep, merged.H4MinEMASep)
}
