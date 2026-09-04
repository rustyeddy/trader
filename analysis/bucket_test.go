package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyZScore(t *testing.T) {
	tests := []struct {
		z    float64
		want Bucket
	}{
		{-3.0, BucketExtremeNegative},
		{-2.0001, BucketExtremeNegative},
		{-2.0, BucketModerateNegative}, // boundary belongs to moderate
		{-1.5, BucketModerateNegative},
		{-1.0001, BucketModerateNegative},
		{-1.0, BucketNeutral}, // boundary belongs to neutral
		{0.0, BucketNeutral},
		{1.0, BucketNeutral}, // boundary belongs to neutral
		{1.0001, BucketModeratePositive},
		{1.5, BucketModeratePositive},
		{2.0, BucketModeratePositive}, // boundary belongs to moderate
		{2.0001, BucketExtremePositive},
		{3.0, BucketExtremePositive},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, ClassifyZScore(tt.z), "z=%v", tt.z)
	}
}

func TestBucket_String(t *testing.T) {
	tests := []struct {
		b    Bucket
		want string
	}{
		{BucketExtremeNegative, "extreme_negative"},
		{BucketModerateNegative, "moderate_negative"},
		{BucketNeutral, "neutral"},
		{BucketModeratePositive, "moderate_positive"},
		{BucketExtremePositive, "extreme_positive"},
		{Bucket(99), "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.b.String())
	}
}

func TestBucket_MarshalText(t *testing.T) {
	text, err := BucketModerateNegative.MarshalText()
	assert.NoError(t, err)
	assert.Equal(t, "moderate_negative", string(text))
}

func TestBucket_UnmarshalText(t *testing.T) {
	for _, want := range Buckets {
		text, err := want.MarshalText()
		require.NoError(t, err)

		var got Bucket
		require.NoError(t, got.UnmarshalText(text))
		assert.Equal(t, want, got)
	}
}

func TestBucket_UnmarshalText_RejectsUnknownLabel(t *testing.T) {
	var b Bucket
	err := b.UnmarshalText([]byte("not_a_real_bucket"))
	assert.ErrorIs(t, err, ErrUnknownBucket)
}

func TestBuckets_CanonicalOrder(t *testing.T) {
	assert.Equal(t, []Bucket{
		BucketExtremeNegative,
		BucketModerateNegative,
		BucketNeutral,
		BucketModeratePositive,
		BucketExtremePositive,
	}, Buckets)
}
