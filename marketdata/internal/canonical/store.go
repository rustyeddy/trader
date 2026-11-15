package canonical

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrNotFound reports a partition with no file; it wraps fs.ErrNotExist and
// names the path that was looked up. Nothing searches another source, stage or
// layout.
var ErrNotFound = errors.New("canonical: partition not found")

// Read reads the partition k from the data root. The path is authoritative: a
// file whose schema comment names another partition is ErrMalformedData.
func Read(ctx context.Context, root string, k Key) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	path, err := k.Path(root)
	if err != nil {
		return File{}, err
	}
	fh, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return File{}, fmt.Errorf("%w: %s: %w", ErrNotFound, path, err)
		}
		return File{}, fmt.Errorf("canonical: %w", err)
	}
	defer func() { _ = fh.Close() }() // read only: nothing to lose
	f, err := Decode(fh)
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	if f.Key != k {
		return File{}, fmt.Errorf("%w: %s: header names %+v, the path names %+v", ErrMalformedData, path, f.Key, k)
	}
	return f, nil
}

// Write atomically writes f as the partition's file under root, replacing any
// file there: a temporary file beside the destination, written, flushed and
// synced, then renamed into place. A failure or cancellation before the rename
// leaves an existing file untouched; a cancellation after it does not undo it.
//
// Canonical is derived data (ADR-020), rebuilt whole from raw, so replacing is
// the intent and no merge is involved, which is why there is no partition lock
// as raw has for its read-merge-write. The contract is one builder per
// partition: two builds of the same inputs differ only in Meta.BuiltAt, but
// which of two concurrent builds ends up visible is not defined. Atomic rename
// guarantees a reader never sees a partial file. Revisit when the storage
// manager (a later phase) owns all file access.
func Write(ctx context.Context, root string, f File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := f.Key.Path(root)
	if err != nil {
		return err
	}
	if err := f.Validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("canonical: write: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("canonical: write: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	bw := bufio.NewWriter(tmp)
	if err := Encode(ctx, bw, f); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("canonical: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("canonical: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("canonical: write: %w", err)
	}
	// The commit point: a cancellation up to this instant leaves path untouched.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("canonical: write: %w", err)
	}
	committed = true
	return nil
}
