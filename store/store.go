// Package store is the one place the data directory is read and written. It
// owns the root of the data directory and offers a handful of byte-oriented
// operations on paths relative to that root: Read, Write, Create, Update and
// List. Write, Create and Update are atomic (a temporary file beside the
// destination, synced, then renamed or linked into place) and hold a per-path
// lock for their whole cycle, so a reader never sees a partial file and two
// overlapping read-merge-replace cycles cannot lose one writer's data.
//
// store knows nothing about formats, instruments or layout: bytes go in and
// come out, and building the relative path is the caller's business (see
// internal/marketdata/layout). It imports only the standard library. Like the
// rest of the domain packages it reads no environment; cmd/trader resolves the
// root from its flags and configuration and calls SetRoot.
//
// The root is process-wide. It defaults to DefaultRoot; tests point it at a
// temporary directory (see store/storetest) and must never leave it at the
// default, which is the real archive.
package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultRoot is the data directory used until SetRoot says otherwise.
const DefaultRoot = "/srv/data"

// Errors, classifiable with errors.Is.
var (
	// ErrInvalidPath reports a path that is not relative to the root or that
	// leaves it: empty, absolute, containing "..", or containing a NUL byte.
	ErrInvalidPath = errors.New("store: invalid path")
	// ErrInvalidRoot reports an empty root.
	ErrInvalidRoot = errors.New("store: invalid root")
)

var current = struct {
	mu   sync.RWMutex
	root string
}{root: DefaultRoot}

// Root returns the current data root.
func Root() string {
	current.mu.RLock()
	defer current.mu.RUnlock()
	return current.root
}

// SetRoot makes dir the data root, as an absolute path so that a relative and
// an absolute spelling of one directory name the same files. It does not create
// the directory.
func SetRoot(dir string) error {
	if dir == "" {
		return ErrInvalidRoot
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRoot, err)
	}
	current.mu.Lock()
	current.root = abs
	current.mu.Unlock()
	return nil
}

// resolve returns the absolute path of the relative path rel under the root.
func resolve(rel string) (string, error) {
	if rel == "" || strings.IndexByte(rel, 0) >= 0 || !filepath.IsLocal(rel) || filepath.Clean(rel) == "." {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, rel)
	}
	return filepath.Join(Root(), rel), nil
}

// Read returns the bytes of the file at the root-relative path. A missing file
// is an error that satisfies errors.Is(err, fs.ErrNotExist).
func Read(path string) ([]byte, error) {
	abs, err := resolve(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

// Write atomically replaces the file at the root-relative path with data,
// creating its directories. It holds the path's lock for the write. A failure or
// a cancellation before the commit leaves an existing file untouched; one that
// arrives after does not undo it.
func Write(ctx context.Context, path string, data []byte) error {
	return commit(ctx, path, func(string) ([]byte, error) { return data, nil }, false)
}

// Create atomically writes data to the root-relative path only if no file is
// there, and returns an error satisfying errors.Is(err, fs.ErrExist) if one is.
// The test and the creation are one atomic step (link(2) fails with EEXIST), not
// a check followed by a rename, so of two concurrent callers exactly one wins.
func Create(ctx context.Context, path string, data []byte) error {
	return commit(ctx, path, func(string) ([]byte, error) { return data, nil }, true)
}

// Update is the safe read-modify-write: it holds the path's lock across reading
// the file (exists is false and old is nil when there is none), calling fn, and
// replacing the file with the result. fn's error leaves the file untouched. fn
// runs under the lock, so it must not call Write, Create or Update on the same
// path. The lock is process-local; two processes are not coordinated.
func Update(ctx context.Context, path string, fn func(old []byte, exists bool) ([]byte, error)) error {
	return commit(ctx, path, func(abs string) ([]byte, error) {
		old, err := os.ReadFile(abs)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return fn(nil, false)
		case err != nil:
			return nil, err
		}
		return fn(old, true)
	}, false)
}

// commit is the shared body of Write, Create and Update: lock, produce the
// content, write it to a synced temporary file beside the destination and
// publish it.
func commit(ctx context.Context, path string, content func(abs string) ([]byte, error), create bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	abs, err := resolve(path)
	if err != nil {
		return err
	}
	unlock, err := lock(ctx, abs)
	if err != nil {
		return err
	}
	defer unlock()

	data, err := content(abs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), "."+filepath.Base(abs)+".tmp-*")
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	tmpPath := tmp.Name()
	published := false
	defer func() {
		_ = tmp.Close()
		if !published {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	// The commit point: a cancellation up to this instant leaves abs untouched.
	if err := ctx.Err(); err != nil {
		return err
	}
	if create {
		if err := os.Link(tmpPath, abs); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("store: %s: %w", path, fs.ErrExist)
			}
			return fmt.Errorf("store: %w", err)
		}
		published = true
		// The temporary name is a second link to the published file; drop it.
		// Best-effort: the durable copy is already at abs.
		_ = os.Remove(tmpPath)
		return nil
	}
	if err := os.Rename(tmpPath, abs); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	published = true
	return nil
}

// List returns the files under the root-relative directory dir (the root itself
// when dir is empty or "."), recursively, as root-relative paths in lexical
// order. Directories are not listed. A missing directory is an error satisfying
// errors.Is(err, fs.ErrNotExist).
func List(dir string) ([]string, error) {
	if dir == "" {
		dir = "."
	}
	if strings.IndexByte(dir, 0) >= 0 || !filepath.IsLocal(dir) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidPath, dir)
	}
	root := Root()
	start := filepath.Join(root, dir)
	var files []string
	err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
