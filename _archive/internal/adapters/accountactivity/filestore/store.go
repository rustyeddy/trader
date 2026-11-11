package filestore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/rustyeddy/trader/internal/accountactivity"
)

const (
	activitiesFile = "activities.jsonl"
	checkpointFile = "checkpoint.json"

	// maxLineBytes bounds one encoded activity line, newline included. The
	// writer rejects any record at or above it and the reader's scanner is
	// sized to it, so the store can never persist a record it cannot read.
	maxLineBytes = 8 * 1024 * 1024
)

// ErrRecordTooLarge reports an activity whose encoded form does not fit the
// store's line limit. Nothing is written for the batch.
var ErrRecordTooLarge = errors.New("filestore: encoded activity exceeds the line size limit")

// fsOps are the durability primitives, behind a seam so tests can inject
// failures and observe ordering. The defaults are real fsyncs.
type fsOps struct {
	syncDir  func(path string) error
	syncFile func(path string) error
}

func realOps() fsOps { return fsOps{syncDir: syncPath, syncFile: syncPath} }

// syncPath opens path (a file or directory), fsyncs it and closes it,
// reporting every failure.
func syncPath(path string) error {
	f, err := os.Open(path) //nolint:gosec // path built by this package from escaped components
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Store is a file-backed accountactivity.Store rooted at one directory.
type Store struct {
	root string

	mu    sync.Mutex
	locks map[accountactivity.SyncKey]*sync.Mutex

	ops     fsOps
	durable map[string]bool // directories already synced successfully; guarded by mu

	// afterActivities, if non-nil, runs after activities are durably stored
	// and before the checkpoint is written. Tests use it to inject a crash.
	afterActivities func() error
}

var _ accountactivity.Store = (*Store)(nil)

// New returns a Store rooted at root, creating the directory if needed.
func New(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("filestore: root must be set")
	}
	s := &Store{root: root, locks: map[accountactivity.SyncKey]*sync.Mutex{}, ops: realOps(), durable: map[string]bool{}}
	if err := s.mkdirAllDurable(root, firstMissing(root)); err != nil {
		return nil, fmt.Errorf("filestore: creating root: %w", err)
	}
	return s, nil
}

// mkdirAllDurable creates path and any missing ancestors, then makes the
// whole directory chain durable: every directory from the filesystem root
// down to path is fsynced, so each entry is persisted by its parent's sync
// and each directory's own metadata by its own.
//
// It deliberately syncs directories that already exist. A directory left by
// an earlier attempt whose parent sync failed is indistinguishable from a
// stable one, so existence proves nothing about durability; only a sync that
// succeeded does. Directories this Store has already synced successfully are
// remembered and skipped, a failure is never remembered, and a new process
// starts empty and re-establishes the chain once.
//
// Directories at or below strictFrom must sync without error. Ancestors above
// it were not created by this store and may be unreadable to this user (a
// 0711 home directory, say), so a permission failure there is tolerated;
// any other failure still propagates.
func (s *Store) mkdirAllDurable(path, strictFrom string) error {
	path, strictFrom = filepath.Clean(path), filepath.Clean(strictFrom)
	if err := os.MkdirAll(path, 0o750); err != nil {
		return err
	}
	var chain []string
	for p := path; ; {
		chain = append([]string{p}, chain...)
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	strict := false
	for _, d := range chain {
		if d == strictFrom {
			strict = true
		}
		s.mu.Lock()
		done := s.durable[d]
		s.mu.Unlock()
		if done {
			continue
		}
		if err := s.ops.syncDir(d); err != nil {
			if !strict && errors.Is(err, fs.ErrPermission) {
				continue
			}
			return fmt.Errorf("syncing %s: %w", d, err)
		}
		s.mu.Lock()
		s.durable[d] = true
		s.mu.Unlock()
	}
	return nil
}

// firstMissing returns the topmost directory of path that does not exist yet
// (the first one the store will create), or path itself if it exists.
func firstMissing(path string) string {
	path = filepath.Clean(path)
	first := path
	for p := path; ; {
		if _, err := os.Stat(p); err == nil {
			return first
		}
		first = p
		parent := filepath.Dir(p)
		if parent == p {
			return first
		}
		p = parent
	}
}

func (s *Store) lock(k accountactivity.SyncKey) func() {
	s.mu.Lock()
	m, ok := s.locks[k]
	if !ok {
		m = &sync.Mutex{}
		s.locks[k] = m
	}
	s.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// dir returns the stream's directory, refusing any component that would
// escape the root.
func (s *Store) dir(k accountactivity.SyncKey) (string, error) {
	parts := []string{k.Provider, k.Environment.String(), k.ProviderAccountID}
	out := make([]string, len(parts))
	for i, p := range parts {
		e := url.PathEscape(p)
		if e == "" || e == "." || e == ".." {
			return "", fmt.Errorf("filestore: %q is not a usable path component", p)
		}
		out[i] = e
	}
	return filepath.Join(append([]string{s.root}, out...)...), nil
}

// Apply implements accountactivity.Store.
func (s *Store) Apply(ctx context.Context, b accountactivity.Batch) (accountactivity.ApplyResult, error) {
	var res accountactivity.ApplyResult
	if err := b.Validate(); err != nil {
		return res, err
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}
	dir, err := s.dir(b.Sync)
	if err != nil {
		return res, err
	}
	defer s.lock(b.Sync)()

	existing, err := loadActivities(dir, b.Sync)
	if err != nil {
		return res, err
	}
	byKey := make(map[accountactivity.Key]accountactivity.Activity, len(existing)+len(b.Activities))
	for _, a := range existing {
		byKey[a.Key()] = a
	}

	// Decide everything before writing anything, so a conflict leaves the
	// store untouched.
	added := make([]accountactivity.Activity, 0, len(b.Activities))
	for _, a := range b.Activities {
		prev, ok := byKey[a.Key()]
		switch {
		case !ok:
			byKey[a.Key()] = a
			added = append(added, a)
			res.Added++
		case prev.SameRecord(a):
			res.Unchanged++
		default:
			return accountactivity.ApplyResult{}, fmt.Errorf("%w: %s", accountactivity.ErrConflict, a.Key())
		}
	}

	if err := s.mkdirAllDurable(dir, s.root); err != nil {
		return accountactivity.ApplyResult{}, fmt.Errorf("filestore: creating stream directory: %w", err)
	}
	if len(added) > 0 {
		all := append(existing, added...)
		sort.SliceStable(all, func(i, j int) bool { return accountactivity.Compare(all[i], all[j]) < 0 })
		if err := s.writeActivities(dir, all); err != nil {
			return accountactivity.ApplyResult{}, err
		}
	} else if err := s.syncActivities(dir); err != nil {
		// Nothing new to write, but the checkpoint is about to claim the
		// activities already present are durable. A retry after a failed
		// sync must establish that, not assume it.
		return accountactivity.ApplyResult{}, err
	}
	if s.afterActivities != nil {
		if err := s.afterActivities(); err != nil {
			return accountactivity.ApplyResult{}, err
		}
	}
	if err := s.writeCheckpoint(dir, accountactivity.Checkpoint{Sync: b.Sync, Cursor: b.Cursor, UpdatedAt: b.At}); err != nil {
		return accountactivity.ApplyResult{}, err
	}
	return res, nil
}

// Checkpoint implements accountactivity.Store.
func (s *Store) Checkpoint(ctx context.Context, k accountactivity.SyncKey) (accountactivity.Checkpoint, bool, error) {
	if err := k.Validate(); err != nil {
		return accountactivity.Checkpoint{}, false, fmt.Errorf("%w: %v", accountactivity.ErrInvalidQuery, err)
	}
	if err := ctx.Err(); err != nil {
		return accountactivity.Checkpoint{}, false, err
	}
	dir, err := s.dir(k)
	if err != nil {
		return accountactivity.Checkpoint{}, false, err
	}
	defer s.lock(k)()

	data, err := os.ReadFile(filepath.Join(dir, checkpointFile))
	if errors.Is(err, fs.ErrNotExist) {
		return accountactivity.Checkpoint{}, false, nil
	}
	if err != nil {
		return accountactivity.Checkpoint{}, false, fmt.Errorf("filestore: reading checkpoint: %w", err)
	}
	var w checkpointWire
	if err := json.Unmarshal(data, &w); err != nil {
		return accountactivity.Checkpoint{}, false, fmt.Errorf("filestore: decoding checkpoint: %w", err)
	}
	cp, err := fromCheckpointWire(w)
	if err != nil {
		return accountactivity.Checkpoint{}, false, fmt.Errorf("filestore: checkpoint: %w", err)
	}
	if cp.Sync != k {
		return accountactivity.Checkpoint{}, false, fmt.Errorf("filestore: checkpoint at %s belongs to %s", k, cp.Sync)
	}
	return cp, true, nil
}

// Query implements accountactivity.Store.
func (s *Store) Query(ctx context.Context, q accountactivity.Query) ([]accountactivity.Activity, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := s.dir(q.Sync)
	if err != nil {
		return nil, err
	}
	defer s.lock(q.Sync)()

	all, err := loadActivities(dir, q.Sync)
	if err != nil {
		return nil, err
	}
	var out []accountactivity.Activity
	for _, a := range all {
		if q.Match(a) {
			out = append(out, a)
		}
	}
	return out, nil
}

// loadActivities reads a stream's activities, validating every line. A
// missing file is an empty stream; a malformed line, an invalid activity, a
// foreign-stream activity, a duplicate key or an out-of-order line is an
// error, never silently skipped.
func loadActivities(dir string, k accountactivity.SyncKey) ([]accountactivity.Activity, error) {
	f, err := os.Open(filepath.Join(dir, activitiesFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("filestore: opening activities: %w", err)
	}
	defer func() { _ = f.Close() }()

	var out []accountactivity.Activity
	seen := map[accountactivity.Key]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for line := 1; sc.Scan(); line++ {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var w activityWire
		if err := json.Unmarshal(sc.Bytes(), &w); err != nil {
			return nil, fmt.Errorf("filestore: %s line %d: %w", activitiesFile, line, err)
		}
		a, err := fromWire(w)
		if err != nil {
			return nil, fmt.Errorf("filestore: %s line %d: %w", activitiesFile, line, err)
		}
		if err := a.Validate(); err != nil {
			return nil, fmt.Errorf("filestore: %s line %d: %w", activitiesFile, line, err)
		}
		if a.SyncKey() != k {
			return nil, fmt.Errorf("filestore: %s line %d belongs to %s, not %s", activitiesFile, line, a.SyncKey(), k)
		}
		if seen[a.Key()] {
			return nil, fmt.Errorf("filestore: %s line %d repeats %s", activitiesFile, line, a.Key())
		}
		seen[a.Key()] = true
		if n := len(out); n > 0 && accountactivity.Compare(out[n-1], a) > 0 {
			return nil, fmt.Errorf("filestore: %s line %d is out of order", activitiesFile, line)
		}
		out = append(out, a)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("filestore: reading activities: %w", err)
	}
	return out, nil
}

func (s *Store) writeActivities(dir string, all []accountactivity.Activity) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, a := range all {
		before := buf.Len()
		if err := enc.Encode(toWire(a)); err != nil {
			return fmt.Errorf("filestore: encoding activity %s: %w", a.Key(), err)
		}
		if buf.Len()-before >= maxLineBytes {
			return fmt.Errorf("%w: %s", ErrRecordTooLarge, a.Key())
		}
	}
	return s.atomicWrite(dir, activitiesFile, buf.Bytes())
}

// syncActivities makes an existing activities file and its directory entry
// durable without rewriting it. A stream with no file yet has nothing to
// establish.
func (s *Store) syncActivities(dir string) error {
	path := filepath.Join(dir, activitiesFile)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("filestore: checking activities: %w", err)
	}
	if err := s.ops.syncFile(path); err != nil {
		return fmt.Errorf("filestore: syncing activities: %w", err)
	}
	if err := s.ops.syncDir(dir); err != nil {
		return fmt.Errorf("filestore: syncing stream directory: %w", err)
	}
	return nil
}

func (s *Store) writeCheckpoint(dir string, cp accountactivity.Checkpoint) error {
	data, err := json.MarshalIndent(toCheckpointWire(cp), "", "  ")
	if err != nil {
		return fmt.Errorf("filestore: encoding checkpoint: %w", err)
	}
	return s.atomicWrite(dir, checkpointFile, append(data, '\n'))
}

// atomicWrite replaces dir/name with data: temp file in dir, fsync, rename,
// then an fsync of the directory whose failure is an error: the rename is not
// durable until the directory entry is, and the checkpoint must never be
// published ahead of the activities it covers.
func (s *Store) atomicWrite(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, name+".*.tmp")
	if err != nil {
		return fmt.Errorf("filestore: creating temporary %s: %w", name, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("filestore: writing %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("filestore: syncing %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("filestore: closing %s: %w", name, err)
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("filestore: publishing %s: %w", name, err)
	}
	if err := s.ops.syncDir(dir); err != nil {
		return fmt.Errorf("filestore: syncing directory after publishing %s: %w", name, err)
	}
	return nil
}
