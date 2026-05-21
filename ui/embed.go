// Package ui exposes the compiled SvelteKit front-end as an embed.FS.
// Run `npm run build` inside ui/ to populate dist/ before building the
// Go binary. The placeholder ui/dist/.gitkeep keeps the directory in git
// so the embed directive compiles even before the first npm build.
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
