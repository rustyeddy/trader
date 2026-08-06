package id

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const idModulePrefix = "github.com/rustyeddy/trader/"

var idLibraryDenylist = map[string]bool{
	"github.com/oklog/ulid":      true,
	"github.com/oklog/ulid/v2":   true,
	"github.com/google/uuid":     true,
	"github.com/gofrs/uuid":      true,
	"github.com/satori/go.uuid":  true,
	"github.com/segmentio/ksuid": true,
	"github.com/rs/xid":          true,
	"github.com/pborman/uuid":    true,
}

var idLibrarySubstrings = []string{"ulid", "uuid", "ksuid", "xid"}

// isThirdPartyIDLibrary reports whether path names a known or
// substring-suspected ID-generation library, excluding anything under this
// module's own import prefix — which is what lets id/internal/ulid, whose
// path itself contains "ulid", pass without special-casing it by name.
func isThirdPartyIDLibrary(path string) bool {
	if strings.HasPrefix(path, idModulePrefix) {
		return false
	}
	if idLibraryDenylist[path] {
		return true
	}
	lower := strings.ToLower(path)
	for _, s := range idLibrarySubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// TestIsThirdPartyIDLibrary is a direct unit test of the detection logic
// TestNoThirdPartyIDLibraryImported relies on, covering both directions:
// known library paths and substring matches must be flagged, and this
// module's own packages — including id/internal/ulid, whose path contains
// "ulid" — must not be.
func TestIsThirdPartyIDLibrary(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"github.com/oklog/ulid/v2", true},
		{"github.com/google/uuid", true},
		{"github.com/some/random-uuid-lib", true},
		{"github.com/whatever/UUID-Generator", true}, // case-insensitive
		{"github.com/rustyeddy/trader/id/internal/ulid", false},
		{"github.com/rustyeddy/trader/clock", false},
		{"github.com/stretchr/testify/assert", false},
		{"encoding/json", false},
		{"crypto/rand", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, isThirdPartyIDLibrary(tt.path))
		})
	}
}

// TestNoThirdPartyIDLibraryImported enforces, mechanically, the
// architecture document's rule for this package: "For public IDs, expose
// Trader-owned types and string encodings. Do not require users to import
// a particular ULID library merely to interact with the API." ULID is
// deliberately reimplemented from scratch in id/internal/ulid rather than
// taken from a dependency, specifically so this constraint holds by
// construction — this test exists to keep it that way if a future change
// ever reaches for a third-party ID library instead.
//
// The check is scoped to this module's own id package files, excluding
// _test.go files, where comparing against a reference implementation would
// be legitimate.
func TestNoThirdPartyIDLibraryImported(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	var violations []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		require.NoError(t, err)

		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if isThirdPartyIDLibrary(path) {
				violations = append(violations, e.Name()+": imports "+path)
			}
		}
	}

	for _, v := range violations {
		assert.Fail(t, "id package must not import a third-party ID-generation library", v)
	}
}
