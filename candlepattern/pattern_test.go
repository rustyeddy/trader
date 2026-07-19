package candlepattern

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRejectionDetector_DefaultKindIsWickRejection(t *testing.T) {
	t.Parallel()
	d, err := GetRejectionDetector(DetectorConfig{}, types.PriceScale)
	require.NoError(t, err)
	assert.Equal(t, "wick-rejection", d.Name())
}

func TestGetRejectionDetector_ExplicitWickRejection(t *testing.T) {
	t.Parallel()
	d, err := GetRejectionDetector(DetectorConfig{Kind: "wick-rejection"}, types.PriceScale)
	require.NoError(t, err)
	assert.Equal(t, "wick-rejection", d.Name())
}

func TestGetRejectionDetector_UnknownKind(t *testing.T) {
	t.Parallel()
	_, err := GetRejectionDetector(DetectorConfig{Kind: "engulfing"}, types.PriceScale)
	assert.ErrorContains(t, err, "engulfing")
}

func TestGetRejectionDetector_UsesDefaultsWhenParamsOmitted(t *testing.T) {
	t.Parallel()
	d, err := GetRejectionDetector(DetectorConfig{}, types.PriceScale)
	require.NoError(t, err)
	wr, ok := d.(*WickRejection)
	require.True(t, ok)
	assert.Equal(t, types.RateFromFloat(0.5), wr.minWickRatio)
	assert.Equal(t, types.RateFromFloat(0.3), wr.maxClosePos)
	assert.Equal(t, types.RateFromFloat(0.5), wr.minWickATR)
	assert.Equal(t, 1, wr.lookback)
}

func TestGetRejectionDetector_OverridesFromParams(t *testing.T) {
	t.Parallel()
	d, err := GetRejectionDetector(DetectorConfig{
		Kind: "wick-rejection",
		Params: map[string]any{
			"min-wick-ratio": 0.6,
			"max-close-pos":  0.2,
			"min-wick-atr":   1.0,
			"lookback":       int64(3),
			"atr-period":     int64(10),
		},
	}, types.PriceScale)
	require.NoError(t, err)
	wr, ok := d.(*WickRejection)
	require.True(t, ok)
	assert.Equal(t, types.RateFromFloat(0.6), wr.minWickRatio)
	assert.Equal(t, types.RateFromFloat(0.2), wr.maxClosePos)
	assert.Equal(t, types.RateFromFloat(1.0), wr.minWickATR)
	assert.Equal(t, 3, wr.lookback)
}

func TestGetRejectionDetector_InvalidParamTypePropagatesError(t *testing.T) {
	t.Parallel()
	_, err := GetRejectionDetector(DetectorConfig{
		Params: map[string]any{"min-wick-ratio": "not-a-number"},
	}, types.PriceScale)
	assert.Error(t, err)
}

func TestGetRejectionDetector_InvalidValuePropagatesConstructorError(t *testing.T) {
	t.Parallel()
	_, err := GetRejectionDetector(DetectorConfig{
		Params: map[string]any{"min-wick-ratio": 5.0}, // out of (0,1]
	}, types.PriceScale)
	assert.ErrorContains(t, err, "min-wick-ratio")
}
