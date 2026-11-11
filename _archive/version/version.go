// Package version holds Trader's own current build/version identity
// (issue #389, ADR-064 — superseding ADR-046's earlier hand-maintained
// const): derived automatically from git tags at build time, never
// bumped by hand.
//
// It is a repository-level, dependency-free package (review finding:
// an earlier version of this package lived under cmd/trader/internal,
// which Go's own internal-visibility rules made unimportable by any
// package outside cmd/trader — directly contradicting issue #389's
// own "version/build metadata is available outside the CLI through a
// small reusable package/API" acceptance criterion), so any consumer
// — backtest run manifests, external-strategy provenance, structured
// startup logs, research artifacts — can import it without depending
// on Cobra or any command package.
//
// gitDescribe is set via -ldflags -X at build time by the Makefile's
// build/install targets, to the output of `git describe --tags
// --match 'v[0-9]*.[0-9]*.[0-9]*' --always --dirty` — restricted to
// Trader's own vMAJOR.MINOR.PATCH release tags (review finding: an
// earlier version of the Makefile used a bare `--tags`, which
// considers every reachable tag in the repository; a future
// unrelated tag, e.g. "research-2026-09", could otherwise become
// Trader's own reported application version depending on
// reachability/distance) — so a binary built from a checkout exactly
// at tag v0.3.0 reports "v0.3.0"; one built 12 commits past that tag
// reports "v0.3.0-12-gabc1234"; a dirty working tree appends
// "-dirty"; a checkout with no matching release tag at all reports a
// bare abbreviated commit hash (git describe's own --always
// fallback). A binary not built through `make build`/`make install`
// (a bare `go build ./cmd/trader`, for example) has no ldflags-
// injected value at all — Current falls back to other mechanisms in
// that case; see its own doc comment.
package version

import (
	"fmt"
	"runtime/debug"
	"strings"
	"time"
)

// gitDescribe is injected via -ldflags -X at build time. Do not set
// this by hand or from anywhere other than the Makefile's build/
// install targets.
var gitDescribe string

// Info is Trader's own current build/version identity.
type Info struct {
	// Version is the primary human-meaningful identifier. See
	// Current's own doc comment for exactly how it is derived and
	// what each of its possible shapes means.
	Version string
	// Revision is the short commit hash Trader was built from, when
	// known — independent of Version, since Version may derive from a
	// mechanism (an exact go install module@vX.Y.Z, for example) that
	// carries no revision of its own. Empty when unknown.
	Revision string
	// CommitTime is Revision's own commit time — when the change was
	// committed, not when the binary was built (review finding: an
	// earlier version of this package's own Report method mislabeled
	// this "built:", but the underlying value, vcs.time from
	// runtime/debug.ReadBuildInfo, has only ever been the commit
	// timestamp; Trader records no separate build-wall-clock
	// timestamp at all, deliberately — commit time plus dirty state
	// plus the git-describe/module version already identify the
	// exact source a binary was built from, which is what matters
	// for reproducibility, and stays stable across two builds of the
	// identical source, unlike a build timestamp would). The zero
	// time.Time when unknown.
	CommitTime time.Time
	// Dirty reports whether the working tree had uncommitted changes
	// at build time, when known.
	Dirty bool
}

// Current returns Trader's own current build/version identity.
//
// Version is resolved with this precedence:
//
//  1. gitDescribe, if the build was made through `make build`/`make
//     install` (the documented, recommended way to build Trader):
//     the exact `git describe` output captured at that build (see
//     the package doc comment for the exact command and its
//     `--match` restriction to Trader's own release-tag namespace).
//  2. Otherwise, the main module's own version from
//     runtime/debug.ReadBuildInfo, if it looks like a real released
//     version rather than Go's own "(devel)" placeholder. This is
//     always true for a version-qualified `go install
//     github.com/rustyeddy/trader/cmd/trader@vX.Y.Z`, which has no
//     local git checkout (and therefore no ldflags from this
//     repository's own Makefile) to derive gitDescribe from; modern
//     Go toolchains (observed on go1.25) may also populate a real
//     main-module pseudo-version for an ordinary local `go build`
//     run inside a git repository, in which case that value is used
//     here too, ahead of case 3 below.
//  3. Otherwise, if VCS build-info is available at all (a bare `go
//     build`/`go install` run from a local git checkout, without
//     going through `make`, on a toolchain that did not already
//     satisfy case 2), a "devel+<revision>[.dirty]" placeholder —
//     clearly not a tag-derived version, but still traceable to the
//     exact commit and working-tree state it was built from.
//  4. "unknown", when none of the above yields anything — a binary
//     built with -buildvcs=false outside any git checkout and
//     outside `make`, for example.
//
// Revision/CommitTime/Dirty always come from VCS build-info when
// available, independent of which case above resolved Version.
func Current() Info {
	return resolveInfo(gitDescribe, readBuildMetadata())
}

// buildMetadata is the runtime-supplied input resolveInfo's own pure
// precedence logic needs — the thin seam between debug.ReadBuildInfo
// (real, environment-dependent, and therefore not directly
// controllable from a test) and resolveInfo (deterministic, table-
// testable) (review finding: an earlier version of this package
// called debug.ReadBuildInfo directly from inside the precedence
// logic itself, making that logic's own branch behavior depend on
// whatever build info the test binary happened to have — never
// controllable, and therefore never actually exercised by a
// deterministic unit test).
type buildMetadata struct {
	// MainVersion is the main module's own version, already reduced
	// to "" when it was Go's "(devel)" placeholder — see
	// mainModuleVersion's own doc comment.
	MainVersion string
	Revision    string
	CommitTime  time.Time
	Dirty       bool
	// VCSOK is true when Revision (and therefore CommitTime/Dirty)
	// were actually found in build info, false when no VCS settings
	// were present at all — the same "ok" readBuildInfo's own vcsInfo
	// helper already returns, carried through here as its own field
	// rather than inferred from Revision == "" (an empty Revision is
	// also possible, in principle, for build info that has VCS
	// settings but genuinely no vcs.revision key).
	VCSOK bool
}

func readBuildMetadata() buildMetadata {
	mainVersion, _ := mainModuleVersion()
	revision, commitTime, dirty, vcsOK := vcsInfo()
	return buildMetadata{
		MainVersion: mainVersion,
		Revision:    revision,
		CommitTime:  commitTime,
		Dirty:       dirty,
		VCSOK:       vcsOK,
	}
}

// resolveInfo implements Current's own documented precedence as a
// pure function of its two inputs, with no I/O and no dependence on
// the calling process's own build info — see buildMetadata's own doc
// comment for why this split exists.
func resolveInfo(gitDescribe string, meta buildMetadata) Info {
	version := "unknown"
	switch {
	case gitDescribe != "":
		version = gitDescribe
	case meta.MainVersion != "":
		version = meta.MainVersion
	case meta.VCSOK:
		version = "devel+" + meta.Revision
		if meta.Dirty {
			version += ".dirty"
		}
	}

	return Info{
		Version:    version,
		Revision:   meta.Revision,
		CommitTime: meta.CommitTime,
		Dirty:      meta.Dirty,
	}
}

// String returns Info's compact, single-line form — Version alone.
// Every case Current's own precedence can produce is already self-
// describing on its own (an exact tag, a git-describe dev string, a
// devel+revision placeholder, or "unknown"), so nothing further is
// appended here; this is what backs Cobra's own --version/-v flag
// (cmd.Version in cmd/trader/internal/rootcmd), whose default
// template is "trader version {{.Version}}". See Report for the
// multi-line form "trader version" (the subcommand) prints, which
// does surface Revision/CommitTime/Dirty explicitly.
func (i Info) String() string {
	return i.Version
}

// Report returns Info's multi-line, human-readable form — what
// "trader version" (the subcommand) prints:
//
//	trader v0.3.0
//	commit: abc1234def012
//	commit-time: 2026-09-15T18:00:00Z
//
// The commit/commit-time lines are omitted entirely when that
// information is unknown, rather than printing a misleading empty or
// zero value. commit-time is exactly that — when Revision was
// committed, not when this binary was built (Info.CommitTime's own
// doc comment explains why Trader records no separate build
// timestamp at all).
func (i Info) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "trader %s\n", i.Version)
	if i.Revision != "" {
		fmt.Fprintf(&b, "commit: %s", i.Revision)
		if i.Dirty {
			b.WriteString(" (dirty)")
		}
		b.WriteString("\n")
	}
	if !i.CommitTime.IsZero() {
		fmt.Fprintf(&b, "commit-time: %s\n", i.CommitTime.Format(time.RFC3339))
	}
	return b.String()
}

// vcsInfo extracts the short commit revision, commit time, and dirty-
// working-tree state from the running binary's own build info, if the
// Go toolchain embedded VCS settings when it was built. ok is false
// when build info is unavailable at all, when it was built with
// -buildvcs=false, when built from a version-qualified module install
// rather than a local VCS checkout (see the package doc comment), or
// when no vcs.revision setting was found for any other reason.
func vcsInfo() (revision string, commitTime time.Time, dirty bool, ok bool) {
	info, available := debug.ReadBuildInfo()
	if !available {
		return "", time.Time{}, false, false
	}

	var haveRevision bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
			haveRevision = true
		case "vcs.time":
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				commitTime = t
			}
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if !haveRevision {
		return "", time.Time{}, false, false
	}
	const shortLen = 12
	if len(revision) > shortLen {
		revision = revision[:shortLen]
	}
	return revision, commitTime, dirty, true
}

// mainModuleVersion returns the running binary's own main-module
// version from build info, when it is a real version rather than Go's
// "(devel)" placeholder — the shape a version-qualified `go install
// module@vX.Y.Z` always produces, and that modern toolchains may also
// produce for an ordinary local `go build` inside a git repository
// (Current's own doc comment, case 2).
func mainModuleVersion() (string, bool) {
	info, available := debug.ReadBuildInfo()
	if !available {
		return "", false
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return "", false
	}
	return v, true
}
