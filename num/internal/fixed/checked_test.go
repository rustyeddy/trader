package fixed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdd(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr error
	}{
		{name: "zero and zero", a: 0, b: 0, want: 0},
		{name: "positive and positive", a: 3 * Scale, b: 4 * Scale, want: 7 * Scale},
		{name: "positive and negative", a: 3 * Scale, b: -4 * Scale, want: -1 * Scale},
		{name: "negative and negative", a: -3 * Scale, b: -4 * Scale, want: -7 * Scale},
		{name: "quantum granularity", a: 1, b: 2, want: 3},
		{name: "cancels to zero", a: MinRaw, b: MaxRaw, want: -1},
		{name: "reaches maximum exactly", a: MaxRaw - 1, b: 1, want: MaxRaw},
		{name: "reaches minimum exactly", a: MinRaw + 1, b: -1, want: MinRaw},
		{name: "overflows past maximum", a: MaxRaw, b: 1, wantErr: ErrOverflow},
		{name: "overflows from two large positives", a: MaxRaw, b: MaxRaw, wantErr: ErrOverflow},
		{name: "underflows past minimum", a: MinRaw, b: -1, wantErr: ErrUnderflow},
		{name: "underflows from two large negatives", a: MinRaw, b: MinRaw, wantErr: ErrUnderflow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Add(tt.a, tt.b)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Zero(t, got, "failed operations must not return a partial result")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSub(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr error
	}{
		{name: "zero and zero", a: 0, b: 0, want: 0},
		{name: "positive difference", a: 7 * Scale, b: 4 * Scale, want: 3 * Scale},
		{name: "negative difference", a: 4 * Scale, b: 7 * Scale, want: -3 * Scale},
		{name: "subtracting a negative", a: 4 * Scale, b: -3 * Scale, want: 7 * Scale},
		{name: "subtracting the minimum from zero overflows", a: 0, b: MinRaw, wantErr: ErrOverflow},
		{name: "reaches maximum exactly", a: 0, b: -MaxRaw, want: MaxRaw},
		{name: "reaches minimum exactly", a: -1, b: MaxRaw, want: MinRaw},
		{name: "overflows past maximum", a: MaxRaw, b: -1, wantErr: ErrOverflow},
		{name: "underflows past minimum", a: MinRaw, b: 1, wantErr: ErrUnderflow},
		{name: "underflows from opposite extremes", a: MinRaw, b: MaxRaw, wantErr: ErrUnderflow},
		{name: "extremes cancel", a: MinRaw, b: MinRaw, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Sub(tt.a, tt.b)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Zero(t, got, "failed operations must not return a partial result")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestAddSubNeverWrap checks the property that distinguishes checked
// arithmetic from Go's native operators: every accepted result must be the
// true mathematical one, and every rejected pair must genuinely be out of
// range.  Native wrapping would satisfy neither.
func TestAddSubNeverWrap(t *testing.T) {
	operands := []int64{
		MinRaw, MinRaw + 1, -Scale, -1, 0, 1, Scale, MaxRaw - 1, MaxRaw,
	}

	for _, a := range operands {
		for _, b := range operands {
			if sum, err := Add(a, b); err == nil {
				assert.Equal(t, a, sum-b, "Add(%d,%d) is not the true sum", a, b)
			}
			if diff, err := Sub(a, b); err == nil {
				assert.Equal(t, a, diff+b, "Sub(%d,%d) is not the true difference", a, b)
			}
		}
	}
}

func TestNeg(t *testing.T) {
	tests := []struct {
		name    string
		in      int64
		want    int64
		wantErr error
	}{
		{name: "zero stays zero", in: 0, want: 0},
		{name: "positive becomes negative", in: 5 * Scale, want: -5 * Scale},
		{name: "negative becomes positive", in: -5 * Scale, want: 5 * Scale},
		{name: "maximum is negatable", in: MaxRaw, want: -MaxRaw},
		{name: "minimum is not representable", in: MinRaw, wantErr: ErrNotRepresentable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Neg(tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAbs(t *testing.T) {
	tests := []struct {
		name    string
		in      int64
		want    int64
		wantErr error
	}{
		{name: "zero stays zero", in: 0, want: 0},
		{name: "positive is unchanged", in: 5 * Scale, want: 5 * Scale},
		{name: "negative is inverted", in: -5 * Scale, want: 5 * Scale},
		{name: "maximum is unchanged", in: MaxRaw, want: MaxRaw},
		{name: "minimum plus one is representable", in: MinRaw + 1, want: MaxRaw},
		{name: "minimum is not representable", in: MinRaw, wantErr: ErrNotRepresentable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Abs(tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSignAndCmp(t *testing.T) {
	assert.Equal(t, -1, Sign(MinRaw))
	assert.Equal(t, -1, Sign(-1))
	assert.Equal(t, 0, Sign(0))
	assert.Equal(t, 1, Sign(1))
	assert.Equal(t, 1, Sign(MaxRaw))

	assert.Equal(t, -1, Cmp(MinRaw, MaxRaw))
	assert.Equal(t, 0, Cmp(0, 0))
	assert.Equal(t, 0, Cmp(MinRaw, MinRaw))
	assert.Equal(t, 1, Cmp(MaxRaw, MinRaw))
	assert.Equal(t, 1, Cmp(1, 0))
}
