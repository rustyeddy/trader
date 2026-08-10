package id

import (
	"testing"

	"github.com/rustyeddy/trader/id/internal/ulid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcreteParseFunctionsMatchTheirKind exercises every one of the six
// Parse<Kind>/MustParse<Kind> wrapper pairs against its own prefix, and
// confirms each rejects a different kind's prefix. This is what would catch
// a copy-paste mistake — say, ParseOrderID wired to fillKind instead of
// orderKind — that a test covering only one or two kinds would miss.
func TestConcreteParseFunctionsMatchTheirKind(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		parse   func(string) (string, error) // returns String() of the parsed ID
		must    func(string) string
		mustBad func(string) // must panic when called with s
	}{
		{"RunID", "run",
			func(s string) (string, error) { v, err := ParseRunID(s); return v.String(), err },
			func(s string) string { return MustParseRunID(s).String() },
			func(s string) { MustParseRunID(s) }},
		{"OrderID", "ord",
			func(s string) (string, error) { v, err := ParseOrderID(s); return v.String(), err },
			func(s string) string { return MustParseOrderID(s).String() },
			func(s string) { MustParseOrderID(s) }},
		{"FillID", "fil",
			func(s string) (string, error) { v, err := ParseFillID(s); return v.String(), err },
			func(s string) string { return MustParseFillID(s).String() },
			func(s string) { MustParseFillID(s) }},
		{"EventID", "evt",
			func(s string) (string, error) { v, err := ParseEventID(s); return v.String(), err },
			func(s string) string { return MustParseEventID(s).String() },
			func(s string) { MustParseEventID(s) }},
		{"CorrelationID", "cor",
			func(s string) (string, error) { v, err := ParseCorrelationID(s); return v.String(), err },
			func(s string) string { return MustParseCorrelationID(s).String() },
			func(s string) { MustParseCorrelationID(s) }},
		{"AccountID", "acc",
			func(s string) (string, error) { v, err := ParseAccountID(s); return v.String(), err },
			func(s string) string { return MustParseAccountID(s).String() },
			func(s string) { MustParseAccountID(s) }},
	}

	var body [16]byte
	body[15] = 1
	encoded := ulid.Encode(body)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			own := tt.prefix + "_" + encoded

			got, err := tt.parse(own)
			require.NoError(t, err)
			assert.Equal(t, own, got)
			assert.Equal(t, own, tt.must(own))

			// Every other kind's prefix must be rejected.
			for _, other := range tests {
				if other.prefix == tt.prefix {
					continue
				}
				wrong := other.prefix + "_" + encoded
				_, err := tt.parse(wrong)
				assert.ErrorIsf(t, err, ErrInvalidID, "%s accepted a %s-prefixed string", tt.name, other.name)
			}

			assert.Panics(t, func() { tt.mustBad("not-a-valid-id") })
		})
	}
}
