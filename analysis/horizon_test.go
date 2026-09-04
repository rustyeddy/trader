package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewH1Horizon(t *testing.T) {
	h, err := NewH1Horizon(4)
	require.NoError(t, err)
	assert.Equal(t, Horizon{Label: "4h", Bars: 4}, h)
}

func TestNewH1Horizon_RejectsNonPositiveHours(t *testing.T) {
	_, err := NewH1Horizon(0)
	assert.ErrorIs(t, err, ErrInvalidHorizon)

	_, err = NewH1Horizon(-1)
	assert.ErrorIs(t, err, ErrInvalidHorizon)
}

func TestMR01Horizons(t *testing.T) {
	got := MR01Horizons()
	want := []Horizon{
		{Label: "4h", Bars: 4},
		{Label: "12h", Bars: 12},
		{Label: "24h", Bars: 24},
		{Label: "48h", Bars: 48},
	}
	assert.Equal(t, want, got)
}
