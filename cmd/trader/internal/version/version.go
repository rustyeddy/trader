// Package version holds Trader's own current build/version identity
// (issue #389, ADR-064 — superseding ADR-046's earlier hand-maintained
// const): derived automatically from git tags at build time, never
// bumped by hand.
//
// gitDescribe is set via -ldflags -X at build time by the Makefile's
// build/install targets, to the output of `git describe --tags
// --always --dirty` — so a binary built from a checkout exactly at tag
// v0.3.0 reports "v0.3.0"; one built 12 commits past that tag reports
// "v0.3.0-12-gabc1234"; a dirty working tree appends "-dirty"; a
// checkout with no tags at all reports a bare abbreviated commit hash
// (git describe's own --always fallback). A binary not built through
// `make build`/`make install` (a bare `go build ./cmd/trader`, for
// example) has no ldflags-injected value at all — Current falls back
// to other mechanisms in that case; see its own doc comment.
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

// Info is Trader's own current build/version identity — a small,
// dependency-free value type importable outside cmd/trader's own CLI
// wiring, so other consumers (backtest run manifests, external-
// strategy provenance, structured startup logs, research artifacts)
// can record the same value without depending on Cobra or any command
// package (issue #389's own "reusable Go API" acceptance criterion).
type Info struct {
	// Version is the primary human-meaningful identifier. See
	// Current's own doc comment for exactly how it is derived and
	// what each of its possible shapes means.
	Version string
	// Revision is the short commit hash Trader was built from, when
	// known — read from the Go toolchain's own automatic VCS build-info
	// stamping (runtime/debug.ReadBuildInfo, present for any build
	// made from a local git checkout regardless of ldflags), never
	// from Version itself: the two are independent, since Version may
	// derive from a mechanism (an exact `go install module@vX.Y.Z`,
	// for example) that carries no revision of its own. Empty when
	// unknown.
	Revision string
	// Time is Revision's own commit time, when known. The zero
	// time.Time when unknown.
	Time time.Time
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
//     the exact `git describe --tags --always --dirty` output
//     captured at that build.
//  2. Otherwise, the main module's own version from
//     runtime/debug.ReadBuildInfo, if it looks like a real released
//     version rather than Go's own "(devel)" placeholder — the shape
//     `go install github.com/rustyeddy/trader/cmd/trader@vX.Y.Z`
//     produces, since that build has no local git checkout (and
//     therefore no ldflags from this repository's own Makefile) to
//     derive gitDescribe from.
//  3. Otherwise, if VCS build-info is available at all (a bare `go
//     build`/`go install` run from a local git checkout, without
//     going through `make`, on a Go toolchain old enough — or
//     configured — not to populate case 2 above for an ordinary
//     local build), a "devel+<revision>[.dirty]" placeholder —
//     clearly not a tag-derived version, but still traceable to the
//     exact commit and working-tree state it was built from.
//  4. "unknown", when none of the above yields anything — a binary
//     built with -buildvcs=false outside any git checkout and outside
//     `make`, for example.
//
// Revision/Time/Dirty always come from VCS build-info when available,
// independent of which case above resolved Version.
func Current() Info {
	revision, commitTime, dirty, vcsOK := vcsInfo()

	version := "unknown"
	switch {
	case gitDescribe != "":
		version = gitDescribe
	default:
		if v, ok := mainModuleVersion(); ok {
			version = v
		} else if vcsOK {
			version = "devel+" + revision
			if dirty {
				version += ".dirty"
			}
		}
	}

	return Info{Version: version, Revision: revision, Time: commitTime, Dirty: dirty}
}

// String returns Info's compact, single-line form — Version alone.
// Every case Current's own precedence can produce is already
// self-describing on its own (an exact tag, a git-describe dev
// string, a devel+revision placeholder, or "unknown"), so nothing
// further is appended here; this is what backs Cobra's own
// --version/-v flag (cmd.Version in cmd/trader/internal/rootcmd),
// whose default template is "trader version {{.Version}}". See
// Report for the multi-line form "trader version" (the subcommand)
// prints, which does surface Revision/Time/Dirty explicitly.
func (i Info) String() string {
	return i.Version
}

// Report returns Info's multi-line, human-readable form — what
// "trader version" (the subcommand) prints:
//
//	trader v0.3.0
//	commit: abc1234def012
//	built: 2026-09-15T18:00:00Z
//
// The commit/built lines are omitted entirely when that information
// is unknown, rather than printing a misleading empty or zero value.
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
	if !i.Time.IsZero() {
		fmt.Fprintf(&b, "built: %s\n", i.Time.Format(time.RFC3339))
	}
	return b.String()
}

// vcsInfo extracts the short commit revision, commit time, and dirty-
// working-tree state from the running binary's own build info, if the
// Go toolchain embedded VCS settings when it was built. ok is false
// when build info is unavailable at all, when it was built with
// -buildvcs=false, when built from a version-qualified module install
// rather than a local VCS checkout (see this package's own doc
// comment), or when no vcs.revision setting was found for any other
// reason.
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
// "(devel)" placeholder — the shape only a version-qualified `go
// install module@vX.Y.Z` produces.
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
