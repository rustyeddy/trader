// Package version holds Trader's own current semantic version (issue
// #288) — a single Go const, bumped manually exactly when a matching
// git tag is created (see the release process in CONTRIBUTING.org) —
// combined at runtime with whatever VCS metadata the Go toolchain
// already embeds in the binary automatically (commit revision,
// dirty-working-tree state), via runtime/debug.ReadBuildInfo.
//
// No ldflags or other build-time injection is required: VCS stamping
// happens automatically for any `go build`/`go install` invocation run
// from within a git working tree (Go 1.18+), so String's output is
// accurate whether the binary was built by `make build`, a bare `go
// build ./cmd/trader`, or `go install
// github.com/rustyeddy/trader/cmd/trader@v0.0.1`.
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
// is unavailable at all (for example, a binary built with
// GOFLAGS=-trimpath in a way that strips it) or when no vcs.revision
// setting was found.
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
