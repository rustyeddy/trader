package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyRegime(t *testing.T) {
	assert.Equal(t, RegimePositive, ClassifyRegime(101, 100))
	assert.Equal(t, RegimeNonPositive, ClassifyRegime(100, 100), "equality is non-positive, not positive")
	assert.Equal(t, RegimeNonPositive, ClassifyRegime(99, 100))
}

func TestRegime_StringAndTextRoundTrip(t *testing.T) {
	for _, r := range []Regime{RegimePositive, RegimeNonPositive} {
		text, err := r.MarshalText()
		require.NoError(t, err)

		var got Regime
		require.NoError(t, got.UnmarshalText(text))
		assert.Equal(t, r, got)
	}
}

func TestRegime_UnmarshalText_UnknownLabel(t *testing.T) {
	var r Regime
	err := r.UnmarshalText([]byte("nope"))
	assert.ErrorIs(t, err, ErrUnknownRegime)
}
