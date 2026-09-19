package version

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCurrent_FallsBackWhenGitDescribeUnset proves Current never
// returns an empty Version, even in a `go test` binary — which does
// not receive ldflags and typically does not get VCS build-info
// stamped either (see vcsInfo's own doc comment) — so it normally
// exercises the "unknown" or main-module-version fallback case,
// never a panic or an empty string.
func TestCurrent_FallsBackWhenGitDescribeUnset(t *testing.T) {
	require.Empty(t, gitDescribe, "this test assumes no ldflags were injected into the test binary")
	got := Current()
	assert.NotEmpty(t, got.Version)
}

// TestCurrent_PrefersGitDescribeWhenSet proves Current's own top
// precedence case: a non-empty gitDescribe (as -ldflags -X would set
// it at build time) is used verbatim as Version, regardless of
// whatever VCS build-info or main-module version might otherwise be
// available.
func TestCurrent_PrefersGitDescribeWhenSet(t *testing.T) {
	old := gitDescribe
	gitDescribe = "v0.3.0-12-gabc1234-dirty"
	t.Cleanup(func() { gitDescribe = old })

	got := Current()
	assert.Equal(t, "v0.3.0-12-gabc1234-dirty", got.Version)
}

// TestInfo_String_IsVersionAlone proves String is exactly Version,
// with no additional formatting — the compact form backing Cobra's
// own --version/-v flag.
func TestInfo_String_IsVersionAlone(t *testing.T) {
	i := Info{Version: "v0.3.0-12-gabc1234"}
	assert.Equal(t, "v0.3.0-12-gabc1234", i.String())
}

// TestInfo_Report_FullyPopulated proves the multi-line report format
// includes every field when all are known, matching issue #389's own
// worked example.
func TestInfo_Report_FullyPopulated(t *testing.T) {
	i := Info{
		Version:  "v0.3.0",
		Revision: "abc1234def012",
		Time:     time.Date(2026, time.September, 15, 18, 0, 0, 0, time.UTC),
		Dirty:    false,
	}
	got := i.Report()
	assert.Equal(t, "trader v0.3.0\ncommit: abc1234def012\nbuilt: 2026-09-15T18:00:00Z\n", got)
}

// TestInfo_Report_MarksDirtyState proves a dirty working tree is
// surfaced on the commit line.
func TestInfo_Report_MarksDirtyState(t *testing.T) {
	i := Info{Version: "v0.3.0-dirty", Revision: "abc1234def012", Dirty: true}
	got := i.Report()
	assert.Contains(t, got, "commit: abc1234def012 (dirty)")
}

// TestInfo_Report_OmitsUnknownFields proves the commit/built lines are
// omitted entirely — not printed with an empty or zero value — when
// that information is unknown, matching every build made without VCS
// build-info available (for example, a version-qualified `go install
// module@vX.Y.Z`).
func TestInfo_Report_OmitsUnknownFields(t *testing.T) {
	i := Info{Version: "v0.3.0"}
	got := i.Report()
	assert.Equal(t, "trader v0.3.0\n", got)
	assert.False(t, strings.Contains(got, "commit:"))
	assert.False(t, strings.Contains(got, "built:"))
}

// TestMainModuleVersion_RejectsDevelPlaceholder proves the "(devel)"
// sentinel Go uses for an ordinary (non version-qualified) build is
// correctly treated as "no real version," not accidentally surfaced
// as one.
func TestMainModuleVersion_RejectsDevelPlaceholder(t *testing.T) {
	// mainModuleVersion reads the real runtime/debug.ReadBuildInfo of
	// this test binary, which — for `go test`, never a version-
	// qualified module install — always reports "(devel)" or no
	// version at all; this asserts that either way, ok is false.
	_, ok := mainModuleVersion()
	assert.False(t, ok, "a `go test` binary must never report a real main-module version")
}
