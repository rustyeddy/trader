//go:build !unix

package store

import "io/fs"

// inodeOf: this platform gives no inode number, so entries are compared by size
// and modification time alone.
func inodeOf(fs.FileInfo) uint64 { return 0 }
