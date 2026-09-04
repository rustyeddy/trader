// Package version holds Trader's own current semantic version (issue
// #288) — a single Go const, bumped manually exactly when a matching
// git tag is created (see the release process in CONTRIBUTING.org) —
// combined at runtime with whatever VCS metadata the Go toolchain
// already embeds in the binary automatically (commit revision,
// dirty-working-tree state), via runtime/debug.ReadBuildInfo.
//
// No ldflags or other build-time injection is required: VCS stamping
// happens automatically for `go build`/`go install` when the main
// package is built directly from a git working tree (Go 1.18+) — so
// `make build`, `make install`, or a bare `go build ./cmd/trader` run
// from a checkout of this repository all get accurate commit/dirty
// info with no extra effort. A version-qualified module install (`go
// install github.com/rustyeddy/trader/cmd/trader@v0.0.1`) is built
// from downloaded module content rather than a local VCS checkout, so
// Go does not stamp vcs.revision/vcs.modified for it (this is a
// documented property of `go install ...@version`, not a bug here);
// String degrades to Version alone in that case, per its own doc
// comment below.
package version

import (
	"fmt"
	"runtime/debug"
)

// Version is Trader's current semantic version. Bump this exactly when
// creating the matching git tag — see CONTRIBUTING.org's release
// process. This is the one source of truth for Trader's own version;
// nothing else in the repository should hardcode it separately.
const Version = "0.0.1"

// String returns Trader's version for display (cmd.Version in
// cmd/trader/internal/rootcmd, printed by Cobra's built-in --version
// flag): Version alone, or Version plus a short VCS revision and a
// "dirty" marker when the Go toolchain's build-info VCS settings are
// available. VCS info is best-effort — its absence (for example, a
// binary built outside any git working tree) is not an error; String
// degrades to Version alone rather than failing.
func String() string {
	revision, dirty, ok := vcsInfo()
	if !ok {
		return Version
	}
	if dirty {
		return fmt.Sprintf("%s (%s, dirty)", Version, revision)
	}
	return fmt.Sprintf("%s (%s)", Version, revision)
}

// vcsInfo extracts the short commit revision and dirty-working-tree
// state from the running binary's own build info, if the Go toolchain
// embedded VCS settings when it was built. ok is false when build info
// is unavailable at all, when it was built with -buildvcs=false (the
// flag that actually disables VCS stamping — not -trimpath, which
// strips file-system paths but leaves VCS settings alone), when built
// from a version-qualified module install rather than a local VCS
// checkout (see this package's own doc comment), or when no
// vcs.revision setting was found for any other reason.
func vcsInfo() (revision string, dirty bool, ok bool) {
	info, available := debug.ReadBuildInfo()
	if !available {
		return "", false, false
	}

	var haveRevision bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
			haveRevision = true
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if !haveRevision {
		return "", false, false
	}
	const shortLen = 12
	if len(revision) > shortLen {
		revision = revision[:shortLen]
	}
	return revision, dirty, true
}
