package backtest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/report"
)

// snapshotSchemaVersion identifies runSnapshot's own on-disk shape
// (issue #222 review, point 4): bumped whenever runSnapshot's fields
// change in a way an older reader could misinterpret, so this
// convenient CLI artifact does not silently become an unversioned,
// unevolvable format.
const snapshotSchemaVersion = 1

// ErrSnapshotVersionMismatch marks a persisted snapshot whose
// SchemaVersion this build of trader does not know how to read.
var ErrSnapshotVersionMismatch = errors.New("cmd/trader/backtest: snapshot schema version mismatch")

// ErrSnapshotRunIDMismatch marks a persisted snapshot whose own
// embedded run identity does not match the run-id its filename was
// loaded under — a renamed or copied file must never silently render
// as a different run than the one requested (issue #222 review,
// point 4).
var ErrSnapshotRunIDMismatch = errors.New("cmd/trader/backtest: snapshot run id does not match requested run id")

// runSnapshot is the durable, versioned artifact "trader backtest run"
// persists and "trader backtest show" reads back — a report-owned
// projection (report.BacktestReport), never a serialization of
// service/backtest.RunResponse or backtest.Result directly (issue #222
// review, point 2): the report projection is already computed once by
// run.go before this is written, so show.go performs zero backtest or
// metric recomputation, only deserialize-then-render.
type runSnapshot struct {
	SchemaVersion int                   `json:"schema_version"`
	Report        report.BacktestReport `json:"report"`
}

// snapshotPath returns dir/<run-id>.json.
func snapshotPath(dir string, runID id.RunID) string {
	return filepath.Join(dir, runID.String()+".json")
}

// saveSnapshot durably writes snap to dir/<run-id>.json, where run-id
// is snap.Report.Run.RunID. It writes to a temporary file in the same
// directory and renames it into place (issue #222 review, point 5) so
// a crash mid-write can never leave a syntactically plausible partial
// artifact at the final path — show.go only ever observes a complete
// file or no file at all.
func saveSnapshot(dir string, snap runSnapshot) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cmd/trader/backtest: creating output directory: %w", err)
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("cmd/trader/backtest: encoding run snapshot: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "run-*.json.tmp")
	if err != nil {
		return fmt.Errorf("cmd/trader/backtest: creating temporary snapshot file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cmd/trader/backtest: writing temporary snapshot file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cmd/trader/backtest: closing temporary snapshot file: %w", err)
	}

	finalPath := filepath.Join(dir, snap.Report.Run.RunID+".json")
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("cmd/trader/backtest: publishing snapshot file: %w", err)
	}
	return nil
}

// loadSnapshot reads and validates dir/<run-id>.json: it must decode,
// its SchemaVersion must match snapshotSchemaVersion, and its own
// embedded Report.Run.RunID must equal runID's canonical string form —
// otherwise a renamed/copied/incompatible file is rejected outright
// rather than silently rendered (issue #222 review, point 4).
func loadSnapshot(dir string, runID id.RunID) (runSnapshot, error) {
	data, err := os.ReadFile(snapshotPath(dir, runID))
	if err != nil {
		return runSnapshot{}, fmt.Errorf("cmd/trader/backtest: reading run snapshot: %w", err)
	}

	var snap runSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return runSnapshot{}, fmt.Errorf("cmd/trader/backtest: decoding run snapshot: %w", err)
	}

	if snap.SchemaVersion != snapshotSchemaVersion {
		return runSnapshot{}, fmt.Errorf("%w: file has version %d, this build reads version %d",
			ErrSnapshotVersionMismatch, snap.SchemaVersion, snapshotSchemaVersion)
	}
	if snap.Report.Run.RunID != runID.String() {
		return runSnapshot{}, fmt.Errorf("%w: file identifies run %s, requested %s",
			ErrSnapshotRunIDMismatch, snap.Report.Run.RunID, runID.String())
	}

	return snap, nil
}
