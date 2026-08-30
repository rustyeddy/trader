// Package jsonl is the JSONL (newline-delimited JSON) storage adapter
// for journal.Recorder/journal.Reader (issue #218, M5-10; ADR-036).
// It is a peer implementation, not part of the storage-neutral journal
// package: a future SQLite/Postgres/etc. adapter would live alongside
// this one, not inside journal itself.
package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/rustyeddy/trader/journal"
)

// Writer is a journal.Recorder that appends one JSON line per Record
// to a file.
//
// # Durability
//
// Writer flushes its buffered writer to the OS after every successful
// Record, so a crash of the Trader process itself (not the OS) loses
// nothing already recorded. Writer calls fsync only in Close, not per
// record: this is an audit trail for a normally completed run, not a
// crash-consistency log for a live trading session — an OS-level crash
// or power loss between two Record calls could still lose the tail of
// buffered-but-unsynced data. This tradeoff is deliberate, not
// accidental; a future live-session journal adapter may need a
// stronger (slower) per-record fsync policy.
//
// # Sequence ownership
//
// Writer assigns each Record's Sequence itself, starting at 1 and
// incrementing by exactly one per successful Record call, under its
// own mutex — see journal.Record's doc comment for why a caller
// cannot supply or influence this value.
type Writer struct {
	mu     sync.Mutex
	file   *os.File
	buf    *bufio.Writer
	seq    uint64
	closed bool
}

// NewWriter creates path and returns a Writer over it. path must not
// already exist: NewWriter always starts Sequence at 1, so silently
// appending to an existing file would either duplicate sequence
// numbers already present in it or mix two runs into one journal,
// either way producing a file that looks well-formed but is not a
// faithful record of any single run. A caller that wants to journal
// another run gives it a fresh path — resuming or extending an
// existing journal file is not a supported use case for this issue.
func NewWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("jsonl: opening %s: %w", path, err)
	}
	return &Writer{file: f, buf: bufio.NewWriter(f)}, nil
}

// Record implements journal.Recorder.
func (w *Writer) Record(ctx context.Context, rec journal.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	validated, err := journal.NewRecord(rec)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return journal.ErrClosed
	}

	entry := journal.Entry{Record: validated, Sequence: w.seq + 1}
	data, err := json.Marshal(toEntryWire(entry))
	if err != nil {
		return fmt.Errorf("jsonl: encoding entry: %w", err)
	}
	if _, err := w.buf.Write(data); err != nil {
		return fmt.Errorf("jsonl: writing entry: %w", err)
	}
	if err := w.buf.WriteByte('\n'); err != nil {
		return fmt.Errorf("jsonl: writing entry: %w", err)
	}
	if err := w.buf.Flush(); err != nil {
		return fmt.Errorf("jsonl: flushing entry: %w", err)
	}
	w.seq++
	return nil
}

// Close implements journal.Recorder: it flushes and fsyncs any
// buffered data, then closes the underlying file. Safe to call more
// than once; every call after the first is a no-op returning nil.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true

	ferr := w.buf.Flush()
	serr := w.file.Sync()
	cerr := w.file.Close()
	if ferr != nil {
		return fmt.Errorf("jsonl: flushing on close: %w", ferr)
	}
	if serr != nil {
		return fmt.Errorf("jsonl: syncing on close: %w", serr)
	}
	if cerr != nil {
		return fmt.Errorf("jsonl: closing file: %w", cerr)
	}
	return nil
}

var _ journal.Recorder = (*Writer)(nil)
