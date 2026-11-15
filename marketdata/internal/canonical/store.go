package canonical

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/rustyeddy/trader/store"
)

// ErrNotFound reports a partition with no file; it wraps fs.ErrNotExist and
// names the path that was looked up. Nothing searches another source, stage or
// layout.
var ErrNotFound = errors.New("canonical: partition not found")

// Read reads the partition k from the data root, through the store. The path is
// authoritative: a file whose schema comment names another partition is
// ErrMalformedData.
func Read(ctx context.Context, k Key) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	path, err := k.Path()
	if err != nil {
		return File{}, err
	}
	data, err := store.Read(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return File{}, fmt.Errorf("%w: %s: %w", ErrNotFound, path, err)
		}
		return File{}, fmt.Errorf("canonical: %w", err)
	}
	f, err := Decode(bytes.NewReader(data))
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	if f.Key != k {
		return File{}, fmt.Errorf("%w: %s: header names %+v, the path names %+v", ErrMalformedData, path, f.Key, k)
	}
	return f, nil
}

// Write atomically writes f as the partition's file under the data root,
// replacing any file there. The store writes a temporary file beside the
// destination, syncs it and renames it into place, under the path's lock, so a
// failure or cancellation before the rename leaves an existing file untouched
// and a reader never sees a partial file; a cancellation after the rename does
// not undo it.
//
// Canonical is derived data (ADR-020), rebuilt whole from raw, so replacing is
// the intent and no merge is involved. The contract is one builder per
// partition: two builds of the same inputs differ only in Meta.BuiltAt.
func Write(ctx context.Context, f File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := f.Key.Path()
	if err != nil {
		return err
	}
	if err := f.Validate(); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := Encode(ctx, &buf, f); err != nil {
		return err
	}
	if err := store.Write(ctx, path, buf.Bytes()); err != nil {
		return fmt.Errorf("canonical: write: %w", err)
	}
	return nil
}
