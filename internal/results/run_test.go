package results

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/id"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRecord(t *testing.T) Record {
	t.Helper()
	runID, err := id.NewGenerator(func() time.Time { return time.Date(2026, 11, 21, 18, 0, 0, 0, time.UTC) }, id.Random{}).RunID()
	require.NoError(t, err)
	rec := Record{
		RunID: runID, Kind: "backtest", Source: "oanda", Interval: "h1", Instruments: []string{"EURUSD"},
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		StartedAt: time.Date(2026, 11, 21, 18, 0, 0, 0, time.UTC),
	}
	rec.Strategy.Name, rec.Strategy.Transport = "emax", "in-process"
	return rec
}

func readRecord(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, RunFile))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func TestStart_MakesTheRunDirectoryAndWritesARunningRecord(t *testing.T) {
	root := filepath.Join(t.TempDir(), "results", "backtest") // the root need not exist
	rec := newRecord(t)

	run, err := Start(root, rec)
	require.NoError(t, err)
	defer func() { _ = run.Finish(nil, nil) }()

	assert.Equal(t, filepath.Join(root, rec.RunID.String()), run.Dir())
	_, err = id.ParseRunID(filepath.Base(run.Dir()))
	require.NoError(t, err, "the directory is named by the RunID")

	m := readRecord(t, run.Dir())
	assert.Equal(t, "running", m["status"], "a run that is cut off says it was running")
	assert.Equal(t, rec.RunID.String(), m["run_id"])
	assert.Equal(t, "backtest", m["kind"])
	assert.Equal(t, map[string]any{"name": "emax", "transport": "in-process"}, m["strategy"])
	assert.Equal(t, "oanda", m["source"])
	assert.Equal(t, []any{"EURUSD"}, m["instruments"])
	assert.Equal(t, "2024-01-01T00:00:00Z", m["from"])
	assert.NotContains(t, m, "ended_at")
	assert.NotContains(t, m, "result")
	for _, name := range []string{EventsFile, AccountFile} {
		info, err := os.Stat(filepath.Join(run.Dir(), name))
		require.NoError(t, err)
		assert.Zero(t, info.Size(), "the journal is open and empty")
	}
}

// The settings given to Start are in run.json while the run is running and however
// it ends.
func TestRun_KeepsTheSettingsHoweverItEnds(t *testing.T) {
	for name, runErr := range map[string]error{
		Completed: nil, Failed: errors.New("boom"), Cancelled: context.Canceled,
	} {
		t.Run(name, func(t *testing.T) {
			rec := newRecord(t)
			rec.Settings = map[string]string{"risk_per_trade": "0.01"}
			want := map[string]any{"risk_per_trade": "0.01"}

			run, err := Start(t.TempDir(), rec)
			require.NoError(t, err)
			assert.Equal(t, want, readRecord(t, run.Dir())["settings"])

			require.ErrorIs(t, run.Finish(runErr, nil), runErr)
			m := readRecord(t, run.Dir())
			assert.Equal(t, name, m["status"])
			assert.Equal(t, want, m["settings"])
		})
	}
}

func TestStart_Refuses(t *testing.T) {
	t.Run("no root", func(t *testing.T) {
		_, err := Start("", newRecord(t))
		assert.ErrorContains(t, err, "no results root")
	})
	t.Run("a root that is a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
		_, err := Start(file, newRecord(t))
		assert.ErrorContains(t, err, "results root")
	})
	t.Run("a run id that already has a directory, which is left alone", func(t *testing.T) {
		root := t.TempDir()
		rec := newRecord(t)
		require.NoError(t, os.Mkdir(filepath.Join(root, rec.RunID.String()), 0o755))
		keep := filepath.Join(root, rec.RunID.String(), "keep.txt")
		require.NoError(t, os.WriteFile(keep, []byte("mine"), 0o644))

		_, err := Start(root, rec)
		assert.ErrorIs(t, err, os.ErrExist)
		data, rerr := os.ReadFile(keep)
		require.NoError(t, rerr)
		assert.Equal(t, "mine", string(data))
		_, serr := os.Stat(filepath.Join(root, rec.RunID.String(), RunFile))
		assert.ErrorIs(t, serr, os.ErrNotExist, "nothing was written into it")
	})
}

func TestFinish(t *testing.T) {
	start := func(t *testing.T) *Run {
		run, err := Start(t.TempDir(), newRecord(t))
		require.NoError(t, err)
		return run
	}
	events := func(t *testing.T, run *Run) string {
		data, err := os.ReadFile(filepath.Join(run.Dir(), EventsFile))
		require.NoError(t, err)
		return string(data)
	}

	t.Run("a completed run keeps its events and its result", func(t *testing.T) {
		run := start(t)
		_, err := run.Journal().Write([]byte(`{"seq":1}` + "\n"))
		require.NoError(t, err)

		require.NoError(t, run.Finish(nil, map[string]int{"trades": 3}))

		m := readRecord(t, run.Dir())
		assert.Equal(t, "completed", m["status"])
		assert.Equal(t, map[string]any{"trades": float64(3)}, m["result"])
		assert.NotContains(t, m, "error")
		ended, err := time.Parse(time.RFC3339, m["ended_at"].(string))
		require.NoError(t, err)
		assert.False(t, ended.Before(newRecord(t).StartedAt))
		assert.Equal(t, `{"seq":1}`+"\n", events(t, run), "what was journaled is on disk")
	})
	t.Run("a failed run keeps the events it had and says why", func(t *testing.T) {
		run := start(t)
		_, err := run.Journal().Write([]byte(`{"seq":1}` + "\n"))
		require.NoError(t, err)

		got := run.Finish(errors.New("worker disconnected"), nil)

		assert.EqualError(t, got, "worker disconnected", "the run's own error comes back")
		m := readRecord(t, run.Dir())
		assert.Equal(t, "failed", m["status"])
		assert.Equal(t, "worker disconnected", m["error"])
		assert.NotContains(t, m, "result")
		assert.Equal(t, `{"seq":1}`+"\n", events(t, run), "the partial journal is kept")
	})
	t.Run("the account journal is kept like the events", func(t *testing.T) {
		run := start(t)
		_, err := run.AccountJournal().Write([]byte(`{"seq":1}` + "\n"))
		require.NoError(t, err)
		require.NoError(t, run.Finish(nil, nil))
		data, err := os.ReadFile(filepath.Join(run.Dir(), AccountFile))
		require.NoError(t, err)
		assert.Equal(t, `{"seq":1}`+"\n", string(data))
	})
	t.Run("an account journal that cannot be flushed fails a run that would have completed", func(t *testing.T) {
		run := start(t)
		_, err := run.AccountJournal().Write([]byte("not on disk yet\n"))
		require.NoError(t, err)
		require.NoError(t, run.account.f.Close())

		got := run.Finish(nil, nil)

		require.ErrorContains(t, got, AccountFile)
		assert.Equal(t, "failed", readRecord(t, run.Dir())["status"])
	})
	t.Run("a cancelled run is cancelled, not failed", func(t *testing.T) {
		run := start(t)
		require.ErrorIs(t, run.Finish(context.Canceled, nil), context.Canceled)
		assert.Equal(t, "cancelled", readRecord(t, run.Dir())["status"])
	})
	t.Run("a journal that cannot be flushed fails a run that would have completed", func(t *testing.T) {
		run := start(t)
		_, err := run.Journal().Write([]byte("not on disk yet\n"))
		require.NoError(t, err)
		require.NoError(t, run.events.f.Close()) // the disk goes away under the buffer

		got := run.Finish(nil, map[string]int{"trades": 1})

		require.ErrorContains(t, got, "journal")
		m := readRecord(t, run.Dir())
		assert.Equal(t, "failed", m["status"])
		assert.Contains(t, m["error"], "journal")
		assert.NotContains(t, m, "result", "a run whose record is incomplete reports no result")
	})
	t.Run("a failed run whose journal also fails says both and keeps its own status", func(t *testing.T) {
		run := start(t)
		_, err := run.Journal().Write([]byte("not on disk yet\n"))
		require.NoError(t, err)
		require.NoError(t, run.events.f.Close())
		disconnect := errors.New("worker disconnected")

		got := run.Finish(disconnect, nil)

		require.ErrorIs(t, got, disconnect)
		assert.ErrorContains(t, got, "journal")
		m := readRecord(t, run.Dir())
		assert.Equal(t, "failed", m["status"])
		assert.Contains(t, m["error"], "worker disconnected")
		assert.Contains(t, m["error"], "the journal is incomplete", "the disconnect is not the only thing wrong with events.jsonl")
	})
	t.Run("a cancelled run whose journal also fails is still cancelled and says so", func(t *testing.T) {
		run := start(t)
		_, err := run.Journal().Write([]byte("not on disk yet\n"))
		require.NoError(t, err)
		require.NoError(t, run.events.f.Close())

		got := run.Finish(context.Canceled, nil)

		require.ErrorIs(t, got, context.Canceled, "the status is classified from the run's own error")
		m := readRecord(t, run.Dir())
		assert.Equal(t, "cancelled", m["status"])
		assert.Contains(t, m["error"], "the journal is incomplete")
	})
	t.Run("run.json is replaced whole and no temporary file is left", func(t *testing.T) {
		run := start(t)
		require.NoError(t, run.Finish(nil, nil))
		entries, err := os.ReadDir(run.Dir())
		require.NoError(t, err)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		assert.ElementsMatch(t, []string{RunFile, EventsFile, AccountFile}, names)
	})
}
