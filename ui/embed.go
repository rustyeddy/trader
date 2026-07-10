// Package ui exposes the compiled SvelteKit front-end as an embed.FS.
// Run `make build-full` from the repository root to rebuild dist/ from Tulip
// before tagging a release. The committed dist/ files are embedded into the Go
// binary by the release workflow.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var FS embed.FS

// SubFS returns an fs.FS rooted at the dist/ subdirectory so callers can
// open "index.html" directly rather than "dist/index.html".
func SubFS() (fs.FS, error) {
	return fs.Sub(FS, "dist")
}
