//go:build unix

package store

import (
	"io/fs"
	"syscall"
)

// inodeOf returns the file's inode number, or zero when the platform gives none.
func inodeOf(info fs.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino) //nolint:gosec,unconvert // an inode number is not a signed quantity; its width differs by platform
	}
	return 0
}
