package filestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// recorder wraps the durability seam: it records every sync and can fail
// chosen ones.
type recorder struct {
	events   []string
	failDir  func(path string) bool
	failFile func(path string) bool
}

func (r *recorder) ops() fsOps {
	return fsOps{
		syncDir: func(p string) error {
			r.events = append(r.events, "dir:"+p)
			if r.failDir != nil && r.failDir(p) {
				return errors.New("injected directory sync failure")
			}
			return nil
		},
		syncFile: func(p string) error {
			r.events = append(r.events, "file:"+filepath.Base(p))
			if r.failFile != nil && r.failFile(p) {
				return errors.New("injected file sync failure")
			}
			return nil
		},
	}
}

func (r *recorder) has(event string) bool {
	for _, e := range r.events {
		if e == event {
			return true
		}
	}
	return false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// The activities' directory entry must be durable before the checkpoint is
// published: if that sync fails, the checkpoint must not appear.
func TestApply_DirectorySyncFailureBlocksTheCheckpoint(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	dir := streamDir(t, s)

	rec := &recorder{}
	rec.failDir = func(p string) bool {
		return p == dir && exists(filepath.Join(dir, activitiesFile)) && !exists(filepath.Join(dir, checkpointFile))
	}
	s.ops = rec.ops()

	_, err := s.Apply(ctx, batch("cursor-1", storetest.Deposit("1", 0)))
	require.Error(t, err)
	assert.False(t, exists(filepath.Join(dir, checkpointFile)), "no checkpoint was published ahead of undurable activities")
	_, ok, err := s.Checkpoint(ctx, storetest.Sync)
	require.NoError(t, err)
	assert.False(t, ok)

	// A retry once the filesystem behaves converges.
	rec.failDir = nil
	res, err := s.Apply(ctx, batch("cursor-1", storetest.Deposit("1", 0)))
	require.NoError(t, err)
	assert.Equal(t, accountactivity.ApplyResult{Unchanged: 1}, res)
	cp, ok, err := s.Checkpoint(ctx, storetest.Sync)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "cursor-1", cp.Cursor)
}

// A retry that finds every activity already stored skips the rewrite, but
// must still establish the durability barrier before advancing.
func TestApply_RetryWithNothingNewStillSyncsBeforeTheCheckpoint(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	dir := streamDir(t, s)
	b := batch("cursor-1", storetest.Deposit("1", 0))
	_, err := s.Apply(ctx, b)
	require.NoError(t, err)

	rec := &recorder{}
	s.ops = rec.ops()
	var before []string
	s.afterActivities = func() error { // runs just before the checkpoint is written
		before = append([]string(nil), rec.events...)
		return nil
	}
	_, err = s.Apply(ctx, batch("cursor-2", storetest.Deposit("1", 0)))
	require.NoError(t, err)
	assert.Contains(t, before, "file:"+activitiesFile, "activities fsynced before the checkpoint moved")
	assert.Contains(t, before, "dir:"+dir, "stream directory fsynced before the checkpoint moved")

	// And a failure of that barrier keeps the checkpoint where it was.
	s.afterActivities = nil
	rec.failFile = func(string) bool { return true }
	_, err = s.Apply(ctx, batch("cursor-3", storetest.Deposit("1", 0)))
	require.Error(t, err)
	cp, _, err := s.Checkpoint(ctx, storetest.Sync)
	require.NoError(t, err)
	assert.Equal(t, "cursor-2", cp.Cursor)
}

func testStore(root string, rec *recorder) *Store {
	return &Store{root: root, ops: rec.ops(), durable: map[string]bool{}, locks: map[accountactivity.SyncKey]*sync.Mutex{}}
}

// The whole directory chain is made durable, from the filesystem root down:
// each directory's entry is persisted by its parent's sync.
func TestMkdirAllDurable_SyncsTheWholeChain(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "a", "b")
	rec := &recorder{}
	s := testStore(root, rec)

	leaf := filepath.Join(root, "oanda", "practice", "acct")
	require.NoError(t, s.mkdirAllDurable(leaf, root))
	for _, want := range []string{
		base, filepath.Join(base, "a"), root,
		filepath.Join(root, "oanda"), filepath.Join(root, "oanda", "practice"), leaf,
	} {
		assert.True(t, rec.has("dir:"+want), "expected a sync of %s", want)
	}

	// Already-synced directories are remembered and not synced again.
	n := len(rec.events)
	require.NoError(t, s.mkdirAllDurable(leaf, root))
	assert.Len(t, rec.events, n)
}

// The causal failure: the child directory is created and its own sync
// succeeds, but the parent sync fails. The directory is left on disk, so a
// retry sees it as existing, and must still establish the parent's sync.
func TestMkdirAllDurable_RetryAfterAFailedParentSync(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	require.NoError(t, os.MkdirAll(root, 0o750))
	child := filepath.Join(root, "oanda")
	leaf := filepath.Join(child, "practice")

	rec := &recorder{}
	rec.failDir = func(p string) bool { return p == child } // child's entry lives in root...
	s := testStore(root, rec)
	require.Error(t, s.mkdirAllDurable(leaf, root))
	assert.True(t, exists(leaf), "the directories from the failed attempt are left behind")
	assert.False(t, s.durable[child], "a failed sync is never remembered")

	// Retry on the same store and on a fresh one (a restarted process): the
	// existing directories are synced again, not skipped.
	for name, st := range map[string]*Store{"same store": s, "fresh store": testStore(root, &recorder{})} {
		t.Run(name, func(t *testing.T) {
			r := &recorder{}
			st.ops = r.ops()
			require.NoError(t, st.mkdirAllDurable(leaf, root))
			assert.True(t, r.has("dir:"+child), "the left-behind directory is synced on retry")
			assert.True(t, r.has("dir:"+leaf))
			assert.True(t, st.durable[child] && st.durable[leaf])
		})
	}
}

// Apply must not publish a checkpoint until the stream's directory chain is
// durable, including when the directories already exist from a failed try.
func TestApply_RetryAfterAFailedDirectoryChainSyncPublishesNothingEarly(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	dir := streamDir(t, s)
	provider := filepath.Dir(filepath.Dir(dir))

	rec := &recorder{}
	rec.failDir = func(p string) bool { return p == provider }
	s.ops = rec.ops()
	_, err := s.Apply(ctx, batch("cursor-1", storetest.Deposit("1", 0)))
	require.Error(t, err)
	assert.True(t, exists(dir), "the stream directory was left behind")
	assert.False(t, exists(filepath.Join(dir, checkpointFile)), "no checkpoint was published")

	rec2 := &recorder{}
	s.ops = rec2.ops()
	_, err = s.Apply(ctx, batch("cursor-1", storetest.Deposit("1", 0)))
	require.NoError(t, err)
	assert.True(t, rec2.has("dir:"+provider), "the retry re-synced the existing directory")
}

func TestMkdirAllDurable_PermissionAboveTheStrictPointIsTolerated(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	rec := &recorder{}
	rec.failDir = func(p string) bool { return p == base }
	s := testStore(root, rec)
	s.ops.syncDir = func(p string) error {
		if p == base {
			return fs.ErrPermission
		}
		return nil
	}
	require.NoError(t, s.mkdirAllDurable(root, root), "an unreadable ancestor we did not create is not fatal")

	// At or below the strict point it is fatal.
	s2 := testStore(root, &recorder{})
	s2.ops.syncDir = func(p string) error {
		if p == root {
			return fs.ErrPermission
		}
		return nil
	}
	assert.Error(t, s2.mkdirAllDurable(root, root))
}

func lineLen(t *testing.T, a accountactivity.Activity) int {
	t.Helper()
	data, err := json.Marshal(toWire(a))
	require.NoError(t, err)
	return len(data) + 1 // the encoder's trailing newline
}

// The writer must never persist a record the reader cannot read.
func TestApply_OversizedRecordIsRejectedNotStored(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	good := storetest.Deposit("1", 0)
	_, err := s.Apply(ctx, batch("cursor-1", good))
	require.NoError(t, err)

	huge := storetest.Deposit("2", time.Hour)
	huge.ProviderType = strings.Repeat("x", maxLineBytes+1)
	_, err = s.Apply(ctx, batch("cursor-2", huge))
	require.ErrorIs(t, err, ErrRecordTooLarge)

	got, err := s.Query(ctx, accountactivity.Query{Sync: storetest.Sync})
	require.NoError(t, err, "the stream is still readable")
	assert.Len(t, got, 1)
	cp, _, err := s.Checkpoint(ctx, storetest.Sync)
	require.NoError(t, err)
	assert.Equal(t, "cursor-1", cp.Cursor, "the checkpoint did not move")
}

// The boundary is exact: the largest accepted line round-trips, one byte
// more is rejected.
func TestApply_LineSizeBoundaryIsExact(t *testing.T) {
	ctx := context.Background()
	base := storetest.Deposit("1", 0)
	base.ProviderType = ""
	pad := maxLineBytes - 1 - lineLen(t, base) // longest line the reader can take

	fits := storetest.Deposit("1", 0)
	fits.ProviderType = strings.Repeat("x", pad)
	require.Equal(t, maxLineBytes-1, lineLen(t, fits))

	s := newStore(t)
	_, err := s.Apply(ctx, batch("c", fits))
	require.NoError(t, err)
	got, err := s.Query(ctx, accountactivity.Query{Sync: storetest.Sync})
	require.NoError(t, err, "the largest accepted record must be readable")
	require.Len(t, got, 1)
	assert.Equal(t, pad, len(got[0].ProviderType))

	over := storetest.Deposit("2", time.Hour)
	over.ProviderType = strings.Repeat("x", pad+1)
	require.Equal(t, maxLineBytes, lineLen(t, over))
	_, err = s.Apply(ctx, batch("c2", over))
	assert.ErrorIs(t, err, ErrRecordTooLarge)
}

// mutateFirstLine rewrites the first activity line through a generic map.
func mutateFirstLine(t *testing.T, dir string, f func(m map[string]any)) {
	t.Helper()
	path := filepath.Join(dir, activitiesFile)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(bytes.SplitN(data, []byte("\n"), 2)[0], &m))
	f(m)
	out, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(out, '\n'), 0o600))
}

// A value object without a payload is corruption; an explicit zero is not.
func TestStore_ValueObjectWithoutPayloadIsCorruption(t *testing.T) {
	ctx := context.Background()
	for name, mutate := range map[string]func(slip map[string]any){
		"missing value": func(slip map[string]any) { delete(slip, "value") },
		"null value":    func(slip map[string]any) { slip["value"] = nil },
	} {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			_, err := s.Apply(ctx, batch("c", storetest.RichFill("1", 0)))
			require.NoError(t, err)
			mutateFirstLine(t, streamDir(t, s), func(m map[string]any) { mutate(m["slippage"].(map[string]any)) })

			_, err = s.Query(ctx, accountactivity.Query{Sync: storetest.Sync})
			require.Error(t, err)
			assert.ErrorContains(t, err, "no value")
		})
	}

	t.Run("an explicit zero stays valid", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Apply(ctx, batch("c", storetest.RichFill("1", 0))) // reported zero financing and P/L
		require.NoError(t, err)
		got, err := s.Query(ctx, accountactivity.Query{Sync: storetest.Sync})
		require.NoError(t, err)
		v, ok := got[0].Financing.Get()
		require.True(t, ok)
		assert.True(t, v.IsZero())
	})
}

// A checkpoint file must satisfy what Batch.Validate demanded on the way in.
func TestStore_InvalidCheckpointIsCorruption(t *testing.T) {
	ctx := context.Background()
	for name, mutate := range map[string]func(m map[string]any){
		"empty cursor":   func(m map[string]any) { m["cursor"] = "" },
		"no cursor":      func(m map[string]any) { delete(m, "cursor") },
		"zero timestamp": func(m map[string]any) { m["updated_at"] = "0001-01-01T00:00:00Z" },
		"no timestamp":   func(m map[string]any) { delete(m, "updated_at") },
	} {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			_, err := s.Apply(ctx, batch("c", storetest.Deposit("1", 0)))
			require.NoError(t, err)

			path := filepath.Join(streamDir(t, s), checkpointFile)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			var m map[string]any
			require.NoError(t, json.Unmarshal(data, &m))
			mutate(m)
			out, err := json.Marshal(m)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, out, 0o600))

			_, ok, err := s.Checkpoint(ctx, storetest.Sync)
			require.Error(t, err)
			assert.False(t, ok, "an invalid checkpoint is never a resume point")
		})
	}
}
