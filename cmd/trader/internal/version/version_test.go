package version

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestString_ContainsVersion proves String always includes Version,
// regardless of whether VCS build-info is available in the test
// binary (it usually is not, since `go test` does not stamp VCS
// settings the same way `go build`/`go install` do — see vcsInfo's
// own doc comment) — String must never omit Version itself.
func TestString_ContainsVersion(t *testing.T) {
	got := String()
	assert.True(t, strings.Contains(got, Version), "String() = %q must contain Version %q", got, Version)
}

// TestVersionConst_IsSemVer proves Version parses as MAJOR.MINOR.PATCH
// with every component a non-negative integer, catching a typo'd bump
// (e.g. "0.0", "0.0.1.0", or "0.0.beta") before it ships.
func TestVersionConst_IsSemVer(t *testing.T) {
	parts := strings.Split(Version, ".")
	require := assert.New(t)
	require.Len(parts, 3, "Version %q must be MAJOR.MINOR.PATCH", Version)
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		require.NoErrorf(err, "Version %q component %q is not a non-negative integer", Version, p)
		require.GreaterOrEqual(n, 0, "Version %q component %q must be non-negative", Version, p)
	}
}
