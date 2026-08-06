package ulid

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeKnownVectors(t *testing.T) {
	tests := []struct {
		name string
		in   [16]byte
		want string
	}{
		{name: "all zero", in: [16]byte{}, want: strings.Repeat("0", 26)},
		{name: "all 0xFF", in: fill(0xFF), want: "7ZZZZZZZZZZZZZZZZZZZZZZZZZ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Encode(tt.in)
			assert.Len(t, got, EncodedLen)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   [16]byte
	}{
		{name: "all zero", in: [16]byte{}},
		{name: "all 0xFF", in: fill(0xFF)},
		{name: "sequential bytes", in: [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}},
		{name: "alternating bits", in: fill(0xAA)},
		{name: "one bit set at top", in: [16]byte{0x80}},
		{name: "one bit set at bottom", in: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := Encode(tt.in)
			require.Len(t, encoded, EncodedLen)

			got, err := Decode(encoded)
			require.NoError(t, err)
			assert.Equal(t, tt.in, got)
		})
	}
}

func TestDecodeRejectsWrongLength(t *testing.T) {
	_, err := Decode("TOOSHORT")
	require.Error(t, err)

	_, err = Decode(Encode([16]byte{}) + "X")
	require.Error(t, err)

	_, err = Decode("")
	require.Error(t, err)
}

func TestDecodeRejectsInvalidCharacters(t *testing.T) {
	// Crockford Base32 excludes I, L, O, and U specifically to avoid
	// confusion with 1 and 0; each must be rejected, not silently mapped.
	for _, c := range []byte{'I', 'L', 'O', 'U', 'i', 'o', '!', ' ', '_'} {
		t.Run(string(c), func(t *testing.T) {
			s := []byte(Encode([16]byte{}))
			s[0] = c
			_, err := Decode(string(s))
			require.Error(t, err)
		})
	}
}

func TestDecodeRejectsLowercase(t *testing.T) {
	// Strict, exact-parsing policy: no case folding, matching num's
	// reject-rather-than-repair convention elsewhere in this module.
	upper := Encode([16]byte{1, 2, 3})
	lower := toLower(upper)
	require.NotEqual(t, upper, lower)

	_, err := Decode(lower)
	require.Error(t, err)
}

func TestDecodeRejectsOverflowingFirstCharacter(t *testing.T) {
	// The first character can only carry 3 significant bits (128 is not a
	// multiple of 5); a value of 8 or more there would require a 129th or
	// 130th bit that a 128-bit value does not have.
	s := []byte(Encode([16]byte{}))
	s[0] = alphabet[8]
	_, err := Decode(string(s))
	require.Error(t, err)

	s[0] = alphabet[31]
	_, err = Decode(string(s))
	require.Error(t, err)
}

func TestNewPacksTimestampAndEntropy(t *testing.T) {
	entropy := [10]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	v, err := New(1_000, entropy)
	require.NoError(t, err)

	assert.Equal(t, entropy, Entropy(v))
	assert.True(t, Timestamp(v).Equal(time.UnixMilli(1_000).UTC()))
}

func TestNewRejectsOutOfRangeTimestamp(t *testing.T) {
	_, err := New(-1, [10]byte{})
	require.Error(t, err)

	_, err = New(maxTimestamp+1, [10]byte{})
	require.Error(t, err)

	_, err = New(maxTimestamp, [10]byte{})
	require.NoError(t, err)

	_, err = New(0, [10]byte{})
	require.NoError(t, err)
}

func TestTimestampExtractionRoundTrip(t *testing.T) {
	want := time.Date(2026, 8, 6, 12, 34, 56, 789_000_000, time.UTC)
	v, err := New(want.UnixMilli(), [10]byte{})
	require.NoError(t, err)

	got := Timestamp(v)
	assert.True(t, got.Equal(want))
	assert.Equal(t, time.UTC, got.Location())
}

func TestIncrementEntropy(t *testing.T) {
	tests := []struct {
		name           string
		in             [10]byte
		wantNext       [10]byte
		wantOverflowed bool
	}{
		{
			name:     "simple increment",
			in:       [10]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			wantNext: [10]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 2},
		},
		{
			name:     "carry across one byte",
			in:       [10]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFF},
			wantNext: [10]byte{0, 0, 0, 0, 0, 0, 0, 0, 1, 0},
		},
		{
			name:     "carry across multiple bytes",
			in:       [10]byte{0, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF, 0xFF},
			wantNext: [10]byte{0, 0, 0, 0, 0, 0, 1, 0, 0, 0},
		},
		{
			name:           "overflow at maximum value",
			in:             fill10(0xFF),
			wantNext:       [10]byte{},
			wantOverflowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, overflowed := IncrementEntropy(tt.in)
			assert.Equal(t, tt.wantOverflowed, overflowed)
			if !overflowed {
				assert.Equal(t, tt.wantNext, next)
			}
		})
	}
}

// FuzzEncodeDecode checks the round trip that must always hold: every
// 128-bit value's canonical encoding must decode back to the identical
// value.
func FuzzEncodeDecode(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(-1))
	f.Add(int64(1<<63 - 1))

	f.Fuzz(func(t *testing.T, seed int64) {
		var v [16]byte
		// Spread the fuzzer's single int64 seed across all 16 bytes with a
		// simple mix, so each run still exercises the full byte range
		// rather than only the low 8 bytes.
		for i := range v {
			seed = seed*6364136223846793005 + 1442695040888963407
			v[i] = byte(seed >> 56)
		}

		got, err := Decode(Encode(v))
		require.NoError(t, err)
		assert.Equal(t, v, got)
	})
}

func fill(b byte) [16]byte {
	var v [16]byte
	for i := range v {
		v[i] = b
	}
	return v
}

func fill10(b byte) [10]byte {
	var v [10]byte
	for i := range v {
		v[i] = b
	}
	return v
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
