package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChoppinessFilter_Name(t *testing.T) {
	t.Parallel()

	f, err := NewChoppinessFilter(14, 61.8, PriceScale)
	require.NoError(t, err)
	assert.Equal(t, "Choppiness(14,61.8)", f.Name())
}

func TestChoppinessFilter_RejectsInvalidThreshold(t *testing.T) {
	t.Parallel()

	tests := []float64{-0.1, 0, 100.1}
	for _, threshold := range tests {
		_, err := NewChoppinessFilter(14, threshold, PriceScale)
		require.Error(t, err)
	}
}

func TestChoppinessFilter_AllowSideAlwaysTrue(t *testing.T) {
	t.Parallel()

	f, err := NewChoppinessFilter(14, 61.8, PriceScale)
	require.NoError(t, err)
	assert.True(t, f.AllowSide(Long))
	assert.True(t, f.AllowSide(Short))
}
