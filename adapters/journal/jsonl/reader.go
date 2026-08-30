package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/rustyeddy/trader/journal"
)

// ErrCorruptEntry reports a JSONL line that could not be decoded into
// a valid journal.Entry — malformed JSON, a truncated final line (a
// process that stopped mid-write), or a value that fails
// journal.NewRecord's own validation on read-back. It is always
// wrapped with enough context (line number, and the underlying decode/
// validation error) to diagnose the corruption; a caller that wants to
// tolerate a truncated last line rather than fail can check
// errors.Is(err, ErrCorruptEntry) and stop reading instead of treating
// it as an unrecoverable error.
var ErrCorruptEntry = errors.New("jsonl: corrupt journal entry")

// Reader is a journal.Reader over one JSONL file.
type Reader struct {
	file    *os.File
	scanner *bufio.Scanner
	line    int
	closed  bool
}

// OpenReader opens path for reading and returns a Reader over it.
func OpenReader(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("jsonl: opening %s: %w", path, err)
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return &Reader{file: f, scanner: scanner}, nil
}

// Next implements journal.Reader. It returns io.EOF once every line in
// the file has been delivered. A line that is present but not valid
// JSON, or decodes to a Record journal.NewRecord itself rejects, is
// reported as ErrCorruptEntry rather than propagated as a bare
// encoding/json error — this is what lets a caller distinguish "the
// file ends here because the file genuinely ends here" from "the file
// ends here because something produced garbage."
func (r *Reader) Next(ctx context.Context) (journal.Entry, error) {
	if err := ctx.Err(); err != nil {
		return journal.Entry{}, err
	}
	if r.closed {
		return journal.Entry{}, journal.ErrClosed
	}

	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			// A scan failure (for example bufio.ErrTooLong on an
			// oversized line) is exactly the kind of malformed-file
			// condition ErrCorruptEntry exists to classify — a caller
			// checking errors.Is(err, ErrCorruptEntry) must not have to
			// separately handle bufio's own error types.
			return journal.Entry{}, fmt.Errorf("%w: line %d: %v", ErrCorruptEntry, r.line+1, err)
		}
		return journal.Entry{}, io.EOF
	}
	r.line++

	var w entryWire
	if err := json.Unmarshal(r.scanner.Bytes(), &w); err != nil {
		return journal.Entry{}, fmt.Errorf("%w: line %d: %v", ErrCorruptEntry, r.line, err)
	}
	entry, err := fromEntryWire(w)
	if err != nil {
		return journal.Entry{}, fmt.Errorf("jsonl: line %d: %w", r.line, err)
	}
	return entry, nil
}

// Close implements journal.Reader. Safe to call more than once.
func (r *Reader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.file.Close()
}

var _ journal.Reader = (*Reader)(nil)
