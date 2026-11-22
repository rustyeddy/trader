// Package results keeps what a run leaves behind: a directory per run under a
// results root, named by its RunID, holding run.json (what the run was and how it
// ended) and events.jsonl (the engine's journal of every order transition). It is
// the one place that opens those files; the engine only writes to an io.Writer it is
// handed, and a client such as Investor only reads the two files.
package results

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/internal/id"
)

// DefaultRoot is where the results of runs are kept when nothing else is
// configured. It is a default, not a place the code knows: the transport resolves
// the root (flag, environment, config file, this) and passes it in.
const DefaultRoot = "/srv/trading/backtest/results"

// Run states, as run.json says them.
const (
	Running   = "running"
	Completed = "completed"
	Failed    = "failed"
	Cancelled = "cancelled"
)

// File names inside a run directory.
const (
	RunFile    = "run.json"     // the Record
	EventsFile = "events.jsonl" // one JSON line per order transition
)

// Record is run.json: what a run was and how it ended. It holds no result of its
// own: Result is the engine's summary, copied as it came, and the orders and fills
// are events.jsonl, the engine's own record. A reader needs only these two files.
type Record struct {
	RunID    id.RunID `json:"run_id"`
	Kind     string   `json:"kind"`
	Strategy struct {
		Name      string `json:"name"`
		Transport string `json:"transport"`
		Addr      string `json:"addr,omitempty"` // the worker's address, for a tcp strategy
	} `json:"strategy"`
	Source      string    `json:"source"`
	Interval    string    `json:"interval"`
	Instruments []string  `json:"instruments"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`

	// StartedAt and EndedAt are wall-clock time. Nothing else in a run's files is: the
	// events carry the simulated time only.
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`

	// Status is Running from the start, so a crash leaves a run that says so, and
	// then Completed, Failed or Cancelled. Error says why for the last two.
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`

	// Result is the engine's summary of a completed run, set by Finish.
	Result any `json:"result,omitempty"`
}

// Run is a run in progress: its directory, its journal file and its record.
type Run struct {
	dir    string
	rec    Record
	events *os.File
	buf    *bufio.Writer
}

// Start makes the run's directory under root, named by the RunID, and opens its
// journal. The directory is created, never reused: a RunID that already has one is
// an error. run.json is written at once with status "running".
func Start(root string, rec Record) (*Run, error) {
	if root == "" {
		return nil, errors.New("no results root")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("results root: %w", err)
	}
	dir := filepath.Join(root, rec.RunID.String())
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, fmt.Errorf("run directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, EventsFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("run journal: %w", err)
	}
	r := &Run{dir: dir, rec: rec, events: f, buf: bufio.NewWriter(f)}
	r.rec.Status = Running
	if err := writeRecord(dir, r.rec); err != nil {
		_ = f.Close()
		return nil, err
	}
	return r, nil
}

// Dir is the run's directory.
func (r *Run) Dir() string { return r.dir }

// Journal is where the engine writes the run's events.
func (r *Run) Journal() io.Writer { return r.buf }

// Finish ends the run: it flushes and closes the journal and rewrites run.json
// with the outcome. runErr is how the run ended (nil for completed), result the
// engine's summary when it completed.
//
// The status is the run's own: completed, cancelled (runErr is context.Canceled) or
// failed. The journal is always flushed and closed, and if that fails too the error
// is added to the run's, never dropped: a completed run becomes failed, since what it
// recorded is not all there, and a failed or cancelled one keeps its status and says
// in its error that events.jsonl may be incomplete. The returned error carries both
// (errors.Is still finds the run's own).
func (r *Run) Finish(runErr error, result any) error {
	var jerr error
	if err := r.buf.Flush(); err != nil {
		jerr = fmt.Errorf("journal: %w", err)
	}
	if err := r.events.Close(); err != nil {
		jerr = errors.Join(jerr, fmt.Errorf("journal: %w", err))
	}

	status := Completed
	switch {
	case runErr == nil && jerr != nil:
		status, runErr, jerr = Failed, jerr, nil
	case runErr == nil:
	case errors.Is(runErr, context.Canceled):
		status = Cancelled
	default:
		status = Failed
	}
	if jerr != nil {
		runErr = fmt.Errorf("%w (and the journal is incomplete: %w)", runErr, jerr)
	}

	r.rec.EndedAt = clock.Clock()
	r.rec.Status = status
	if runErr != nil {
		r.rec.Error = runErr.Error()
	} else {
		r.rec.Result = result
	}
	if err := writeRecord(r.dir, r.rec); err != nil {
		return errors.Join(runErr, err)
	}
	return runErr
}

// writeRecord replaces run.json with rec: written to a temporary file, synced and
// renamed over it, so a reader sees the old record or the new one and never half.
func writeRecord(dir string, rec Record) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("run record: %w", err)
	}
	tmp := filepath.Join(dir, RunFile+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("run record: %w", err)
	}
	_, werr := f.Write(append(data, '\n'))
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("run record: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, RunFile)); err != nil {
		return fmt.Errorf("run record: %w", err)
	}
	return nil
}
