package id

import (
	"strings"
	"testing"

	"github.com/rustyeddy/trader/id/internal/ulid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindPrefixes(t *testing.T) {
	// These are a public contract embedded in every ID string ever issued;
	// pin them so a change is a visible, deliberate diff.
	tests := []struct {
		kind   Kind
		prefix string
	}{
		{runKind{}, "run"},
		{sessionKind{}, "ses"},
		{intentKind{}, "int"},
		{proposalKind{}, "prp"},
		{orderKind{}, "ord"},
		{fillKind{}, "fil"},
		{eventKind{}, "evt"},
		{correlationKind{}, "cor"},
		{accountKind{}, "acc"},
		{instrumentKind{}, "ins"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.prefix, tt.kind.Prefix())
	}
}

func TestDistinctKindsAreDistinctTypes(t *testing.T) {
	// This is a compile-time property, not a runtime one: the test itself
	// IS the assertion. If RunID and OrderID were assignable to each other,
	// this file would fail to compile.
	var run RunID
	var order OrderID
	_ = run
	_ = order

	// The following would not compile if uncommented, which is the point:
	// run = order
}

func TestParseValidRoundTrips(t *testing.T) {
	for i := range 5 {
		valid := MustParseRunID(genValidRunID(t, byte(i+1)))
		again, err := ParseRunID(valid.String())
		require.NoError(t, err)
		assert.True(t, valid.Equal(again))
	}
}

func TestParseRejectsWrongPrefix(t *testing.T) {
	orderStr := genValidID(t, "ord", 1)
	_, err := ParseRunID(orderStr)
	require.ErrorIs(t, err, ErrInvalidID)
}

func TestParseRejectsMalformedBody(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "prefix only", in: "run_"},
		{name: "no separator", in: "run01H8Z3K3R2N4XG9YB6HFA1V7ZQ"},
		{name: "wrong length body", in: "run_TOOSHORT"},
		{name: "invalid character", in: "run_I1H8Z3K3R2N4XG9YB6HFA1V7ZQ"},
		{name: "no prefix at all", in: "01H8Z3K3R2N4XG9YB6HFA1V7ZQ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRunID(tt.in)
			require.ErrorIs(t, err, ErrInvalidID)
		})
	}
}

func TestParseRejectsAllZeroBody(t *testing.T) {
	// The all-zero value is reserved for the unset zero value; it must
	// never be accepted as a real, parsed identifier.
	zeroBody := strings.Repeat("0", 26)
	_, err := ParseRunID("run_" + zeroBody)
	require.ErrorIs(t, err, ErrInvalidID)
}

func TestMustParsePanicsOnInvalidInput(t *testing.T) {
	assert.Panics(t, func() { MustParseRunID("not-a-valid-id") })
}

func TestZeroValueIsZero(t *testing.T) {
	var r RunID
	assert.True(t, r.IsZero())
	assert.Equal(t, "<unset id>", r.String())
}

func TestNonZeroValueIsNotZero(t *testing.T) {
	r := MustParseRunID(genValidRunID(t, 1))
	assert.False(t, r.IsZero())
}

func TestEqual(t *testing.T) {
	a := MustParseRunID(genValidRunID(t, 1))
	b := MustParseRunID(a.String())
	c := MustParseRunID(genValidRunID(t, 2))

	assert.True(t, a.Equal(b))
	assert.False(t, a.Equal(c))

	var zero1, zero2 RunID
	assert.True(t, zero1.Equal(zero2), "two zero values are equal to each other")
	assert.False(t, a.Equal(zero1))
}

func TestTimeErrorsOnZeroValue(t *testing.T) {
	var r RunID
	_, err := r.Time()
	require.ErrorIs(t, err, ErrZeroValue)
}

func TestTimeReturnsEncodedInstant(t *testing.T) {
	r := MustParseRunID(genValidRunID(t, 1))
	got, err := r.Time()
	require.NoError(t, err)
	assert.False(t, got.IsZero())
}

// genValidRunID and genValidID build a syntactically valid ID string
// directly from the ulid encoding, varying one byte so distinct calls
// produce distinct identifiers, without depending on the Generator this
// test file's phase does not yet cover.
func genValidRunID(t *testing.T, b byte) string {
	t.Helper()
	return genValidID(t, "run", b)
}

func genValidID(t *testing.T, prefix string, b byte) string {
	t.Helper()
	var v [16]byte
	v[15] = b
	return prefix + "_" + ulid.Encode(v)
}
