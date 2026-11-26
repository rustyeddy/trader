package stooq

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/rustyeddy/trader/internal/store"
)

// ErrInput reports an input that cannot be read as asked: a ZIP without a member
// name or without that member, a member name for a file that is not a ZIP, or a
// file too large to be one symbol's daily history.
var ErrInput = errors.New("stooq: unusable input")

// maxSnapshot bounds one symbol's daily history. SPY's twenty years are under
// half a megabyte.
const maxSnapshot = 64 << 20

// Snapshot is the exact bytes of one symbol's whole history, and where they came
// from.
type Snapshot struct {
	// Data is the file's bytes, or the uncompressed bytes of the ZIP member,
	// unchanged.
	Data []byte
	// File is the name of the file read, without its directory.
	File string
	// Member is the path of the member inside the ZIP, empty otherwise.
	Member string
	// ArchiveSHA256 is the SHA-256 of the whole ZIP ("sha256:<hex>"), empty
	// otherwise.
	ArchiveSHA256 string
}

// ReadSnapshot reads one symbol's history from the root-relative file: the member
// of a ZIP when the name ends in ".zip", the whole file otherwise. The ZIP is
// hashed and read through one open handle, so the hash is of the archive the
// member came from. A missing file is an error satisfying
// errors.Is(err, fs.ErrNotExist).
func ReadSnapshot(ctx context.Context, file, member string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	zipped := strings.EqualFold(path.Ext(file), ".zip")
	switch {
	case zipped && member == "":
		return Snapshot{}, fmt.Errorf("%w: %s is a ZIP: name the member to read", ErrInput, file)
	case !zipped && member != "":
		return Snapshot{}, fmt.Errorf("%w: a member was named but %s is not a ZIP", ErrInput, file)
	}
	f, err := store.Open(file)
	if err != nil {
		return Snapshot{}, fmt.Errorf("stooq: %w", err)
	}
	defer func() { _ = f.Close() }() // read only: nothing to lose
	out := Snapshot{File: path.Base(file), Member: member}
	if !zipped {
		out.Data, err = readBounded(f, file)
		return out, err
	}

	h := sha256.New()
	size, err := io.Copy(h, contextReader{ctx, f})
	if err != nil {
		return Snapshot{}, fmt.Errorf("stooq: %s: %w", file, err)
	}
	out.ArchiveSHA256 = "sha256:" + hex.EncodeToString(h.Sum(nil))
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %s: %w", ErrInput, file, err)
	}
	var found []*zip.File
	for _, m := range zr.File {
		if m.Name == member {
			found = append(found, m)
		}
	}
	if len(found) != 1 {
		return Snapshot{}, fmt.Errorf("%w: %s has %d members named %q, want one", ErrInput, file, len(found), member)
	}
	r, err := found[0].Open()
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %s: %s: %w", ErrInput, file, member, err)
	}
	defer func() { _ = r.Close() }()
	out.Data, err = readBounded(r, file+": "+member)
	if err != nil {
		return Snapshot{}, err
	}
	return out, ctx.Err()
}

// readBounded reads all of r, refusing more than maxSnapshot bytes. A ZIP member
// is checked against its stored CRC as it is read to the end.
func readBounded(r io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxSnapshot+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInput, name, err)
	}
	if len(data) > maxSnapshot {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrInput, name, maxSnapshot)
	}
	return data, nil
}

// contextReader stops a long read when its context is cancelled.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
