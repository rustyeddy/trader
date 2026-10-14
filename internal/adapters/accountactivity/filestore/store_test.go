package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/accountactivity/storetest"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	require.NoError(t, err)
	return s
}

func TestStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) accountactivity.Store { return newStore(t) })
}

func batch(cursor string, as ...accountactivity.Activity) accountactivity.Batch {
	return accountactivity.Batch{Sync: storetest.Sync, Activities: as, Cursor: cursor, At: time.Date(2024, 3, 7, 9, 0, 0, 0, time.UTC)}
}

func streamDir(t *testing.T, s *Store) string {
	t.Helper()
	d, err := s.dir(storetest.Sync)
	require.NoError(t, err)
	return d
}

// A crash after the activities are stored but before the checkpoint moves
// must leave the checkpoint where it was, and retrying the same batch must
// converge without duplicates.
func TestApply_RetryAfterPartialFailure(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	_, err := s.Apply(ctx, batch("cursor-1", storetest.Deposit("1", 0)))
	require.NoError(t, err)

	next := batch("cursor-2", storetest.Deposit("2", time.Hour), storetest.RichFill("3", 2*time.Hour))
	crash := errors.New("simulated crash between activities and checkpoint")
	s.afterActivities = func() error { return crash }
	_, err = s.Apply(ctx, next)
	require.ErrorIs(t, err, crash)

	cp, ok, err := s.Checkpoint(ctx, storetest.Sync)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "cursor-1", cp.Cursor, "the checkpoint must not move past activities it did not finish")

	stored, err := s.Query(ctx, accountactivity.Query{Sync: storetest.Sync})
	require.NoError(t, err)
	assert.Len(t, stored, 3, "the activities of the failed batch are durable")

	s.afterActivities = nil
	res, err := s.Apply(ctx, next)
	require.NoError(t, err)
	assert.Equal(t, accountactivity.ApplyResult{Unchanged: 2}, res, "the retry finds them already stored")

	cp, _, err = s.Checkpoint(ctx, storetest.Sync)
	require.NoError(t, err)
	assert.Equal(t, "cursor-2", cp.Cursor)
	stored, err = s.Query(ctx, accountactivity.Query{Sync: storetest.Sync})
	require.NoError(t, err)
	assert.Len(t, stored, 3, "no duplicates after the retry")
}

// A store reopened on the same root sees everything: it is durable, not an
// in-process cache.
func TestStore_SurvivesReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := New(root)
	require.NoError(t, err)
	_, err = s.Apply(ctx, batch("cursor-9", storetest.RichFill("1", 0)))
	require.NoError(t, err)

	again, err := New(root)
	require.NoError(t, err)
	got, err := again.Query(ctx, accountactivity.Query{Sync: storetest.Sync})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, storetest.RichFill("1", 0).SameRecord(got[0]))
	cp, ok, err := again.Checkpoint(ctx, storetest.Sync)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "cursor-9", cp.Cursor)
}

// The same set of activities produces byte-identical files whatever order
// and batching delivered them.
func TestStore_FileIsDeterministic(t *testing.T) {
	ctx := context.Background()
	a, b := newStore(t), newStore(t)
	_, err := a.Apply(ctx, batch("c", storetest.Deposit("1", 0), storetest.Deposit("2", time.Hour), storetest.RichFill("3", 2*time.Hour)))
	require.NoError(t, err)
	_, err = b.Apply(ctx, batch("x", storetest.RichFill("3", 2*time.Hour)))
	require.NoError(t, err)
	_, err = b.Apply(ctx, batch("c", storetest.Deposit("2", time.Hour), storetest.Deposit("1", 0)))
	require.NoError(t, err)

	fa, err := os.ReadFile(filepath.Join(streamDir(t, a), activitiesFile))
	require.NoError(t, err)
	fb, err := os.ReadFile(filepath.Join(streamDir(t, b), activitiesFile))
	require.NoError(t, err)
	assert.Equal(t, string(fa), string(fb))
}

func TestStore_LeavesNoTemporaryFiles(t *testing.T) {
	s := newStore(t)
	_, err := s.Apply(context.Background(), batch("c", storetest.Deposit("1", 0)))
	require.NoError(t, err)
	entries, err := os.ReadDir(streamDir(t, s))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), ".tmp"), e.Name())
	}
	assert.Len(t, entries, 2)
}

// Corruption is reported, never skipped over.
func TestStore_CorruptionIsAnError(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(t *testing.T, dir string){
		"malformed line": func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, activitiesFile), []byte("{not json\n"), 0o600))
		},
		"wrong schema": func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, activitiesFile), []byte(`{"schema":99}`+"\n"), 0o600))
		},
		"duplicate key": func(t *testing.T, dir string) {
			data, err := os.ReadFile(filepath.Join(dir, activitiesFile))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, activitiesFile), append(data, data...), 0o600))
		},
		"foreign stream": func(t *testing.T, dir string) {
			data, err := os.ReadFile(filepath.Join(dir, activitiesFile))
			require.NoError(t, err)
			data = []byte(strings.Replace(string(data), storetest.Sync.ProviderAccountID, "someone-else", 1))
			require.NoError(t, os.WriteFile(filepath.Join(dir, activitiesFile), data, 0o600))
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			_, err := s.Apply(ctx, batch("c", storetest.Deposit("1", 0)))
			require.NoError(t, err)
			corrupt(t, streamDir(t, s))

			_, err = s.Query(ctx, accountactivity.Query{Sync: storetest.Sync})
			require.Error(t, err)
			_, err = s.Apply(ctx, batch("c2", storetest.Deposit("2", time.Hour)))
			require.Error(t, err, "an import must not build on a corrupt ledger")
		})
	}

	t.Run("checkpoint for another stream", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Apply(ctx, batch("c", storetest.Deposit("1", 0)))
		require.NoError(t, err)
		path := filepath.Join(streamDir(t, s), checkpointFile)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(data), storetest.Sync.ProviderAccountID, "other", 1)), 0o600))
		_, _, err = s.Checkpoint(ctx, storetest.Sync)
		require.Error(t, err)
	})
}

// A provider account ID is external input and must not name a path outside
// the store root.
func TestStore_AccountIDCannotEscapeTheRoot(t *testing.T) {
	s := newStore(t)
	for _, id := range []string{"..", ".", "../../etc", "a/b", `a\b`} {
		k := accountactivity.SyncKey{Provider: "oanda", Environment: accountactivity.EnvironmentLive, ProviderAccountID: id}
		d, err := s.dir(k)
		if err == nil {
			rel, relErr := filepath.Rel(s.root, d)
			require.NoError(t, relErr)
			assert.False(t, strings.HasPrefix(rel, ".."), "%q resolved to %s outside the root", id, d)
			assert.Equal(t, 3, len(strings.Split(rel, string(filepath.Separator))), "%q must stay three components deep", id)
		}
	}
	_, err := s.dir(accountactivity.SyncKey{Provider: "oanda", Environment: accountactivity.EnvironmentLive, ProviderAccountID: ".."})
	assert.Error(t, err)
}

func TestNew_RequiresARoot(t *testing.T) {
	_, err := New("")
	assert.Error(t, err)
}
