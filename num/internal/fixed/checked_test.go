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
		{name: "positive and positive", a: 3 * scale, b: 4 * scale, want: 7 * scale},
		{name: "positive and negative", a: 3 * scale, b: -4 * scale, want: -1 * scale},
		{name: "negative and negative", a: -3 * scale, b: -4 * scale, want: -7 * scale},
		{name: "quantum granularity", a: 1, b: 2, want: 3},
		{name: "cancels to zero", a: minRaw, b: maxRaw, want: -1},
		{name: "reaches maximum exactly", a: maxRaw - 1, b: 1, want: maxRaw},
		{name: "reaches minimum exactly", a: minRaw + 1, b: -1, want: minRaw},
		{name: "overflows past maximum", a: maxRaw, b: 1, wantErr: ErrOverflow},
		{name: "overflows from two large positives", a: maxRaw, b: maxRaw, wantErr: ErrOverflow},
		{name: "underflows past minimum", a: minRaw, b: -1, wantErr: ErrUnderflow},
		{name: "underflows from two large negatives", a: minRaw, b: minRaw, wantErr: ErrUnderflow},
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
		{name: "positive difference", a: 7 * scale, b: 4 * scale, want: 3 * scale},
		{name: "negative difference", a: 4 * scale, b: 7 * scale, want: -3 * scale},
		{name: "subtracting a negative", a: 4 * scale, b: -3 * scale, want: 7 * scale},
		{name: "subtracting the minimum from zero overflows", a: 0, b: minRaw, wantErr: ErrOverflow},
		{name: "reaches maximum exactly", a: 0, b: -maxRaw, want: maxRaw},
		{name: "reaches minimum exactly", a: -1, b: maxRaw, want: minRaw},
		{name: "overflows past maximum", a: maxRaw, b: -1, wantErr: ErrOverflow},
		{name: "underflows past minimum", a: minRaw, b: 1, wantErr: ErrUnderflow},
		{name: "underflows from opposite extremes", a: minRaw, b: maxRaw, wantErr: ErrUnderflow},
		{name: "extremes cancel", a: minRaw, b: minRaw, want: 0},
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
		minRaw, minRaw + 1, -scale, -1, 0, 1, scale, maxRaw - 1, maxRaw,
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
		{name: "positive becomes negative", in: 5 * scale, want: -5 * scale},
		{name: "negative becomes positive", in: -5 * scale, want: 5 * scale},
		{name: "maximum is negatable", in: maxRaw, want: -maxRaw},
		{name: "minimum is not representable", in: minRaw, wantErr: ErrNotRepresentable},
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
		{name: "positive is unchanged", in: 5 * scale, want: 5 * scale},
		{name: "negative is inverted", in: -5 * scale, want: 5 * scale},
		{name: "maximum is unchanged", in: maxRaw, want: maxRaw},
		{name: "minimum plus one is representable", in: minRaw + 1, want: maxRaw},
		{name: "minimum is not representable", in: minRaw, wantErr: ErrNotRepresentable},
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
	assert.Equal(t, -1, Sign(minRaw))
	assert.Equal(t, -1, Sign(-1))
	assert.Equal(t, 0, Sign(0))
	assert.Equal(t, 1, Sign(1))
	assert.Equal(t, 1, Sign(maxRaw))

	assert.Equal(t, -1, Cmp(minRaw, maxRaw))
	assert.Equal(t, 0, Cmp(0, 0))
	assert.Equal(t, 0, Cmp(minRaw, minRaw))
	assert.Equal(t, 1, Cmp(maxRaw, minRaw))
	assert.Equal(t, 1, Cmp(1, 0))
}
