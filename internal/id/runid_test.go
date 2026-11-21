package id

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/id/internal/ulid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validRunID builds a well formed RunID string, varying one byte so distinct calls
// give distinct IDs, without going through a Generator.
func validRunID(b byte) string {
	var v [16]byte
	v[15] = b
	return "run_" + ulid.Encode(v)
}

func TestParseRunID_RoundTrips(t *testing.T) {
	for i := range 5 {
		id, err := ParseRunID(validRunID(byte(i + 1)))
		require.NoError(t, err)
		again, err := ParseRunID(id.String())
		require.NoError(t, err)
		assert.True(t, id.Equal(again))
		assert.Equal(t, validRunID(byte(i+1)), id.String())
	}
}

func TestParseRunID_Rejects(t *testing.T) {
	tests := []struct{ name, in string }{
		{"empty", ""},
		{"prefix only", "run_"},
		{"no separator", "run01H8Z3K3R2N4XG9YB6HFA1V7ZQ"},
		{"short body", "run_TOOSHORT"},
		{"invalid character", "run_I1H8Z3K3R2N4XG9YB6HFA1V7ZQ"},
		{"no prefix", "01H8Z3K3R2N4XG9YB6HFA1V7ZQ"},
		{"another kind's prefix", "ord_" + strings.TrimPrefix(validRunID(1), "run_")},
		{"the reserved all-zero value", "run_" + strings.Repeat("0", 26)},
		{"a directory name with a suffix", validRunID(1) + "-copy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRunID(tt.in)
			require.ErrorIs(t, err, ErrInvalidID)
		})
	}
}

func TestMustParseRunID(t *testing.T) {
	assert.Equal(t, validRunID(1), MustParseRunID(validRunID(1)).String())
	assert.Panics(t, func() { MustParseRunID("not-a-valid-id") })
}

func TestRunID_ZeroValue(t *testing.T) {
	var r RunID
	assert.True(t, r.IsZero())
	assert.Equal(t, "<unset id>", r.String())
	assert.False(t, MustParseRunID(validRunID(1)).IsZero())

	_, err := r.Time()
	require.ErrorIs(t, err, ErrZeroValue)
	_, err = r.MarshalText()
	require.ErrorIs(t, err, ErrZeroValue)
	_, err = json.Marshal(r)
	require.Error(t, err)
}

func TestRunID_Equal(t *testing.T) {
	a := MustParseRunID(validRunID(1))
	assert.True(t, a.Equal(MustParseRunID(a.String())))
	assert.False(t, a.Equal(MustParseRunID(validRunID(2))))

	var zero RunID
	assert.True(t, zero.Equal(RunID{}), "two zero values are equal")
	assert.False(t, a.Equal(zero))
}

func TestRunID_Time(t *testing.T) {
	at := time.Date(2026, 11, 21, 18, 15, 0, 123_000_000, time.UTC)
	id, err := NewGenerator(func() time.Time { return at }, NewDeterministic(1, 2)).RunID()
	require.NoError(t, err)

	got, err := id.Time()
	require.NoError(t, err)
	assert.True(t, got.Equal(at), "the creation time to the millisecond: %s", got)
	assert.Equal(t, time.UTC, got.Location())
}

func TestRunID_Text(t *testing.T) {
	want := MustParseRunID(validRunID(7))
	text, err := want.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, want.String(), string(text))

	var got RunID
	require.NoError(t, got.UnmarshalText(text))
	assert.True(t, want.Equal(got))

	require.ErrorIs(t, got.UnmarshalText([]byte("not-a-valid-id")), ErrInvalidID)
	require.ErrorIs(t, got.UnmarshalText([]byte("ord_"+strings.TrimPrefix(validRunID(1), "run_"))), ErrInvalidID)
}

func TestRunID_JSON(t *testing.T) {
	type record struct {
		RunID RunID `json:"run_id"`
	}
	want := record{RunID: MustParseRunID(validRunID(9))}

	data, err := json.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, `{"run_id":"`+validRunID(9)+`"}`, string(data), "a string, never the raw bytes")

	var got record
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, want.RunID.Equal(got.RunID))

	var r RunID
	require.ErrorIs(t, json.Unmarshal([]byte(`"not-a-valid-id"`), &r), ErrInvalidID)
	require.Error(t, json.Unmarshal([]byte(`12345`), &r))
}
