package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrExists reports that a directory move would land on something that is already
// there. The store never overwrites a directory.
var ErrExists = errors.New("store: destination exists")

// Entry describes one file under the root, enough to tell afterwards whether it is
// the same file: a rename keeps the inode, size and modification time.
type Entry struct {
	// Path is relative to the root.
	Path string
	Size int64
	// ModTime is the file's modification time, as the filesystem has it.
	ModTime time.Time
	// Inode identifies the file on its filesystem (with the device, which a move
	// within one root does not change). It is zero where the platform gives none.
	Inode uint64
	// Symlink is true for a symbolic link, which is recorded, never followed.
	Symlink bool
}

// Inventory lists every file under the root-relative directory dir, recursively,
// in lexical order of path, with what is needed to recognise it again. Directories
// are not listed; a symbolic link is listed with Symlink set and not followed. A
// missing directory is an error satisfying errors.Is(err, fs.ErrNotExist). It
// stops with the context's error if ctx is cancelled.
func Inventory(ctx context.Context, dir string) ([]Entry, error) {
	abs, err := resolve(dir)
	if err != nil {
		return nil, err
	}
	root := Root()
	var out []Entry
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, Entry{Path: rel, Size: info.Size(), ModTime: info.ModTime(), Inode: inodeOf(info),
			Symlink: info.Mode()&fs.ModeSymlink != 0})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Exists reports whether anything is at the root-relative path, without following
// a symbolic link.
func Exists(path string) (bool, error) {
	abs, err := resolve(path)
	if err != nil {
		return false, err
	}
	_, err = os.Lstat(abs)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, fmt.Errorf("store: %w", err)
}

// Move renames the directory at the root-relative oldDir to newDir, creating newDir's
// parents. It is one rename(2) on one filesystem, so it is atomic and takes no
// longer for a million files than for one, and every file keeps its inode and its
// bytes. It refuses, with ErrExists, to move onto anything that is already there,
// even an empty directory (rename(2) would replace that silently), and it holds
// the destination's lock for the move. A missing oldDir is an error satisfying
// errors.Is(err, fs.ErrNotExist). Cancellation before the rename leaves everything
// as it was.
func Move(ctx context.Context, oldDir, newDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	from, err := resolve(oldDir)
	if err != nil {
		return err
	}
	to, err := resolve(newDir)
	if err != nil {
		return err
	}
	unlock, err := lock(ctx, to)
	if err != nil {
		return err
	}
	defer unlock()

	info, err := os.Lstat(from)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("store: %s is not a directory", oldDir)
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, newDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// Open opens the file at the root-relative path for reading, for a caller that
// needs random access to a file too large to read whole, such as an archive. The
// caller closes it. A missing file is an error satisfying
// errors.Is(err, fs.ErrNotExist).
func Open(path string) (*os.File, error) {
	abs, err := resolve(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return f, nil
}

// Checksum returns the SHA-256 of the file at the root-relative path, as lower-case
// hex, reading it in a stream. It stops with the context's error if ctx is
// cancelled between reads.
func Checksum(ctx context.Context, path string) (string, error) {
	abs, err := resolve(path)
	if err != nil {
		return "", err
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("store: %w", err)
	}
	defer func() { _ = f.Close() }() // read only: nothing to lose
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, rerr := f.Read(buf)
		h.Write(buf[:n])
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", fmt.Errorf("store: %w", rerr)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Subdirs returns the names of the directories directly inside the root-relative
// directory dir (the root itself when dir is empty or "."), in lexical order. Files
// and symbolic links are not listed. A missing directory is an error satisfying
// errors.Is(err, fs.ErrNotExist).
func Subdirs(dir string) ([]string, error) {
	abs := Root()
	if dir != "" && dir != "." {
		var err error
		if abs, err = resolve(dir); err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() { // DirEntry.IsDir is false for a symbolic link
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// IsTemp reports whether a file name is one of the store's own temporary files,
// which a Write, Create or Update leaves beside its destination for the moment
// between creating and publishing it. Finding one means a write is in flight or
// was interrupted.
func IsTemp(name string) bool {
	base := filepath.Base(name)
	return len(base) > 0 && base[0] == '.' && strings.Contains(base, ".tmp-")
}
