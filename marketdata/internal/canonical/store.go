package canonical

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/rustyeddy/trader/internal/store"
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

// Outcome says what Publish did to the partition's file.
type Outcome string

// The outcomes of Publish.
const (
	// Built: there was no file, and one was written.
	Built Outcome = "built"
	// Unchanged: a file with the same build (raw fingerprint, builder, validator
	// and availability policy) was already there, so nothing was written.
	Unchanged Outcome = "unchanged"
	// Replaced: a file was there that came from a different build, or could not
	// be read as canonical-v2, and it was replaced.
	Replaced Outcome = "replaced"
)

// Publish makes f the partition's file, doing only what is needed (ADR-090 s4).
// Under the file's lock it compares the file on disk with f: when the build is
// the same (Meta.SameBuild, which ignores BuiltAt) it writes nothing; when there
// is none it writes one; when the file is from another build, or is not readable
// as canonical-v2 (an old canonical-v1 file, say), it replaces it. Canonical is
// derived data, so replacing is safe and raw is never touched. The write is the
// store's atomic one; a cancellation before the commit leaves any file as it was.
func Publish(ctx context.Context, f File) (Outcome, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := f.Key.Path()
	if err != nil {
		return "", err
	}
	if err := f.Validate(); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := Encode(ctx, &buf, f); err != nil {
		return "", err
	}
	outcome := Built
	err = store.Update(ctx, path, func(old []byte, exists bool) ([]byte, error) {
		if !exists {
			return buf.Bytes(), nil
		}
		prev, derr := Decode(bytes.NewReader(old))
		if derr == nil && prev.Key == f.Key && prev.Meta.SameBuild(f.Meta) {
			outcome = Unchanged
			return nil, store.ErrUnchanged
		}
		outcome = Replaced
		return buf.Bytes(), nil
	})
	if err != nil {
		return "", fmt.Errorf("canonical: publish: %w", err)
	}
	return outcome, nil
}
