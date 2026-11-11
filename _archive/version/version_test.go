package version

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveInfo_Precedence is a deterministic table test of
// Current's own documented precedence, driven directly against
// resolveInfo's pure logic with a synthetic buildMetadata — never
// against the real, environment-dependent debug.ReadBuildInfo (review
// finding: an earlier version of this suite could only directly
// control the gitDescribe branch; mainModuleVersion/vcsInfo's own
// behavior depended on whatever the test binary's own build info
// happened to be, so the devel+revision and module-version fallback
// branches were never actually exercised deterministically).
func TestResolveInfo_Precedence(t *testing.T) {
	commitTime := time.Date(2026, time.September, 15, 18, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		gitDescribe string
		meta        buildMetadata
		wantVersion string
	}{
		{
			name:        "exact injected tag wins over everything else",
			gitDescribe: "v0.3.0",
			meta:        buildMetadata{MainVersion: "v9.9.9", Revision: "abc123", CommitTime: commitTime, VCSOK: true},
			wantVersion: "v0.3.0",
		},
		{
			name:        "injected git-describe dev string wins over module version",
			gitDescribe: "v0.3.0-12-gabc1234-dirty",
			meta:        buildMetadata{MainVersion: "v9.9.9", VCSOK: true},
			wantVersion: "v0.3.0-12-gabc1234-dirty",
		},
		{
			name:        "module version used when gitDescribe empty",
			gitDescribe: "",
			meta:        buildMetadata{MainVersion: "v0.3.0", Revision: "abc123", VCSOK: true},
			wantVersion: "v0.3.0",
		},
		{
			name:        "vcs revision fallback when neither gitDescribe nor module version present",
			gitDescribe: "",
			meta:        buildMetadata{Revision: "abc123def456", VCSOK: true},
			wantVersion: "devel+abc123def456",
		},
		{
			name:        "dirty vcs fallback appends .dirty",
			gitDescribe: "",
			meta:        buildMetadata{Revision: "abc123def456", Dirty: true, VCSOK: true},
			wantVersion: "devel+abc123def456.dirty",
		},
		{
			name:        "complete unknown case: no gitDescribe, no module version, no VCS",
			gitDescribe: "",
			meta:        buildMetadata{VCSOK: false},
			wantVersion: "unknown",
		},
		{
			name:        "VCSOK false with a stray Revision is still unknown, not devel+ (defensive: MainVersion/gitDescribe both empty)",
			gitDescribe: "",
			meta:        buildMetadata{Revision: "should-not-be-used", VCSOK: false},
			wantVersion: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveInfo(tt.gitDescribe, tt.meta)
			assert.Equal(t, tt.wantVersion, got.Version)
			// Revision/CommitTime/Dirty always come from meta directly,
			// independent of which precedence case resolved Version.
			assert.Equal(t, tt.meta.Revision, got.Revision)
			assert.True(t, tt.meta.CommitTime.Equal(got.CommitTime))
			assert.Equal(t, tt.meta.Dirty, got.Dirty)
		})
	}
}

// TestCurrent_NeverEmpty proves Current (the real, environment-backed
// entry point) always returns a non-empty Version end to end, however
// its own environment happens to resolve — this is an integration
// smoke test, not a substitute for TestResolveInfo_Precedence's own
// deterministic coverage of the actual branch logic.
func TestCurrent_NeverEmpty(t *testing.T) {
	require.Empty(t, gitDescribe, "this test assumes no ldflags were injected into the test binary")
	got := Current()
	assert.NotEmpty(t, got.Version)
}

// TestInfo_String_IsVersionAlone proves String is exactly Version,
// with no additional formatting — the compact form backing Cobra's
// own --version/-v flag.
func TestInfo_String_IsVersionAlone(t *testing.T) {
	i := Info{Version: "v0.3.0-12-gabc1234"}
	assert.Equal(t, "v0.3.0-12-gabc1234", i.String())
}

// TestInfo_Report_FullyPopulated proves the multi-line report format
// includes every field when all are known, using "commit-time:" (not
// "built:") for the commit timestamp — review finding: vcs.time is
// the commit time, not a build wall-clock timestamp Trader does not
// separately record.
func TestInfo_Report_FullyPopulated(t *testing.T) {
	i := Info{
		Version:    "v0.3.0",
		Revision:   "abc1234def012",
		CommitTime: time.Date(2026, time.September, 15, 18, 0, 0, 0, time.UTC),
		Dirty:      false,
	}
	got := i.Report()
	assert.Equal(t, "trader v0.3.0\ncommit: abc1234def012\ncommit-time: 2026-09-15T18:00:00Z\n", got)
}

// TestInfo_Report_MarksDirtyState proves a dirty working tree is
// surfaced on the commit line.
func TestInfo_Report_MarksDirtyState(t *testing.T) {
	i := Info{Version: "v0.3.0-dirty", Revision: "abc1234def012", Dirty: true}
	got := i.Report()
	assert.Contains(t, got, "commit: abc1234def012 (dirty)")
}

// TestInfo_Report_OmitsUnknownFields proves the commit/commit-time
// lines are omitted entirely — not printed with an empty or zero
// value — when that information is unknown, matching every build
// made without VCS build-info available (for example, a version-
// qualified `go install module@vX.Y.Z`).
func TestInfo_Report_OmitsUnknownFields(t *testing.T) {
	i := Info{Version: "v0.3.0"}
	got := i.Report()
	assert.Equal(t, "trader v0.3.0\n", got)
	assert.False(t, strings.Contains(got, "commit:"))
	assert.False(t, strings.Contains(got, "commit-time:"))
}

// TestMainModuleVersion_RejectsDevelPlaceholder proves the "(devel)"
// sentinel Go uses for an ordinary (non version-qualified) build is
// correctly treated as "no real version," not accidentally surfaced
// as one. This one test necessarily depends on the real build info of
// the `go test` binary running it (unlike TestResolveInfo_Precedence,
// which needs no such dependency) — it exists specifically to prove
// mainModuleVersion's own "(devel)" special case, which cannot be
// exercised through the synthetic buildMetadata seam at all.
func TestMainModuleVersion_RejectsDevelPlaceholder(t *testing.T) {
	_, ok := mainModuleVersion()
	assert.False(t, ok, "a `go test` binary must never report a real main-module version")
}
