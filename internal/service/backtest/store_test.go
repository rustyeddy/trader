package backtest_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/internal/report"
	svcbacktest "github.com/rustyeddy/trader/internal/service/backtest"
)

// successfulReport runs the package's fixture backtest and assembles its
// report.
func successfulReport(t *testing.T) (svcbacktest.RunResponse, report.BacktestReport) {
	t.Helper()
	svc, err := svcbacktest.New(newFixtureManager(t), newFixtureResolver(t), simEnvironmentFactory{}, nil)
	require.NoError(t, err)
	resp, err := svc.Run(context.Background(), validRunRequest(t))
	require.NoError(t, err)
	return resp, svcbacktest.NewReport(resp)
}

func TestNewReport_ProjectsTheRunResponse(t *testing.T) {
	resp, rep := successfulReport(t)
	want := report.NewBacktestReport(report.BacktestInput{
		Manifest: resp.Manifest, Account: resp.Account, Trades: resp.Trades, OpenTrades: resp.OpenTrades,
		EquityCurve: resp.EquityCurve, Metrics: resp.Metrics, MarginRejections: resp.MarginRejections,
	})
	assert.Equal(t, want, rep)
	assert.Equal(t, resp.Manifest.RunID().String(), rep.Run.RunID)
}

func TestRunStore_SaveThenLoad(t *testing.T) {
	_, rep := successfulReport(t)
	dir := filepath.Join(t.TempDir(), "runs") // created on demand
	store := svcbacktest.NewRunStore(dir)
	assert.Equal(t, dir, store.Dir())

	require.NoError(t, store.Save(rep))
	runID, err := id.ParseRunID(rep.Run.RunID)
	require.NoError(t, err)
	got, err := store.Load(runID)
	require.NoError(t, err)

	wantJSON, err := json.Marshal(rep)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, string(wantJSON), string(gotJSON), "the report round-trips")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary file is left behind")
	assert.Equal(t, rep.Run.RunID+".json", entries[0].Name())
}

func TestRunStore_LoadFailures(t *testing.T) {
	_, rep := successfulReport(t)
	runID, err := id.ParseRunID(rep.Run.RunID)
	require.NoError(t, err)

	t.Run("not found", func(t *testing.T) {
		_, err := svcbacktest.NewRunStore(t.TempDir()).Load(runID)
		assert.ErrorIs(t, err, svcbacktest.ErrRunNotFound)
	})

	write := func(t *testing.T, dir string, name string, snapshot map[string]any) {
		t.Helper()
		b, err := json.Marshal(snapshot)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), b, 0o644))
	}
	t.Run("schema version mismatch", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, rep.Run.RunID+".json", map[string]any{"schema_version": svcbacktest.SnapshotSchemaVersion + 1, "report": rep})
		_, err := svcbacktest.NewRunStore(dir).Load(runID)
		assert.ErrorIs(t, err, svcbacktest.ErrSnapshotVersionMismatch)
	})
	t.Run("run id mismatch", func(t *testing.T) {
		dir := t.TempDir()
		other := rep
		other.Run.RunID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
		write(t, dir, rep.Run.RunID+".json", map[string]any{"schema_version": svcbacktest.SnapshotSchemaVersion, "report": other})
		_, err := svcbacktest.NewRunStore(dir).Load(runID)
		assert.ErrorIs(t, err, svcbacktest.ErrSnapshotRunIDMismatch)
	})
	t.Run("corrupt file", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, rep.Run.RunID+".json"), []byte("{"), 0o644))
		_, err := svcbacktest.NewRunStore(dir).Load(runID)
		assert.ErrorContains(t, err, "decoding run snapshot")
	})
}

func TestRunStore_SaveFailsWhenDirectoryCannotBeCreated(t *testing.T) {
	_, rep := successfulReport(t)
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	assert.Error(t, svcbacktest.NewRunStore(filepath.Join(file, "runs")).Save(rep))
}
