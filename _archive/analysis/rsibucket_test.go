package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClassifyRSIBucket_Boundaries proves the frozen EQR-01 bucket
// edges are inclusive-lower/exclusive-upper except B6, which is
// additionally closed at 100 — docs/research/eqr-01-research-protocol.org's
// own explicit resolution of the source issue's ambiguous "0-5, 5-10, ..."
// list.
func TestClassifyRSIBucket_Boundaries(t *testing.T) {
	tests := []struct {
		rsi  float64
		want RSIBucket
	}{
		{0, RSIBucketB1},
		{4.999, RSIBucketB1},
		{5, RSIBucketB2},
		{9.999, RSIBucketB2},
		{10, RSIBucketB3},
		{19.999, RSIBucketB3},
		{20, RSIBucketB4},
		{29.999, RSIBucketB4},
		{30, RSIBucketB5},
		{49.999, RSIBucketB5},
		{50, RSIBucketB6},
		{99.999, RSIBucketB6},
		{100, RSIBucketB6},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, ClassifyRSIBucket(tt.rsi), "rsi=%v", tt.rsi)
	}
}

func TestRSIBucket_StringAndTextRoundTrip(t *testing.T) {
	for _, b := range RSIBuckets {
		text, err := b.MarshalText()
		require.NoError(t, err)

		var got RSIBucket
		require.NoError(t, got.UnmarshalText(text))
		assert.Equal(t, b, got)
	}
}

func TestRSIBucket_UnmarshalText_UnknownLabel(t *testing.T) {
	var b RSIBucket
	err := b.UnmarshalText([]byte("nope"))
	assert.ErrorIs(t, err, ErrUnknownRSIBucket)
}

func TestRSIBuckets_CanonicalOrder(t *testing.T) {
	require.Len(t, RSIBuckets, 6)
	assert.Equal(t, []RSIBucket{
		RSIBucketB1, RSIBucketB2, RSIBucketB3, RSIBucketB4, RSIBucketB5, RSIBucketB6,
	}, RSIBuckets)
}
