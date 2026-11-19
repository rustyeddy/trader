package store_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRoot_IsTheArchive(t *testing.T) {
	assert.Equal(t, "/srv/data", store.DefaultRoot)
}

func TestSetRoot_ChangesAndRestores(t *testing.T) {
	prev := store.Root()
	t.Cleanup(func() { require.NoError(t, store.SetRoot(prev)) })
	dir := t.TempDir()

	require.NoError(t, store.SetRoot(dir))

	assert.Equal(t, dir, store.Root())
}

func TestSetRoot_RelativeBecomesAbsolute(t *testing.T) {
	prev := store.Root()
	t.Cleanup(func() { require.NoError(t, store.SetRoot(prev)) })

	require.NoError(t, store.SetRoot("some/dir"))

	assert.True(t, filepath.IsAbs(store.Root()))
}

func TestSetRoot_RejectsEmpty(t *testing.T) {
	prev := store.Root()

	err := store.SetRoot("")

	require.ErrorIs(t, err, store.ErrInvalidRoot)
	assert.Equal(t, prev, store.Root(), "a rejected root changes nothing")
}

func TestWriteRead_RoundTripsBytesAndCreatesDirectories(t *testing.T) {
	storetest.Root(t)
	want := []byte("# schema\ntime,price\n2024-01-02T00:00:00Z,1.1\n")

	require.NoError(t, store.Write(context.Background(), "a/b/c.csv", want))
	got, err := store.Read("a/b/c.csv")

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestWrite_ReplacesAnExistingFile(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	require.NoError(t, store.Write(ctx, "f.csv", []byte("old")))

	require.NoError(t, store.Write(ctx, "f.csv", []byte("new")))

	got, err := store.Read("f.csv")
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
}

func TestWrite_LeavesNoTemporaryFile(t *testing.T) {
	root := storetest.Root(t)
	require.NoError(t, store.Write(context.Background(), "d/f.csv", []byte("x")))

	entries, err := os.ReadDir(filepath.Join(root, "d"))

	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "f.csv", entries[0].Name())
}

func TestWrite_CancelledBeforeCommitLeavesTheFileUntouched(t *testing.T) {
	root := storetest.Root(t)
	require.NoError(t, store.Write(context.Background(), "f.csv", []byte("old")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Write(ctx, "f.csv", []byte("new"))

	require.ErrorIs(t, err, context.Canceled)
	got, rerr := os.ReadFile(filepath.Join(root, "f.csv"))
	require.NoError(t, rerr)
	assert.Equal(t, "old", string(got))
}

func TestRead_MissingFileIsNotExist(t *testing.T) {
	storetest.Root(t)

	_, err := store.Read("nope.csv")

	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestPaths_AreRelativeToTheRootAndCannotEscape(t *testing.T) {
	root := storetest.Root(t)
	ctx := context.Background()
	outside := filepath.Join(filepath.Dir(root), "outside.csv")
	for name, path := range map[string]string{
		"empty":     "",
		"absolute":  outside,
		"parent":    "../outside.csv",
		"nested up": "a/../../outside.csv",
		"dot":       ".",
		"dot dir":   "a/..",
		"nul":       "a\x00b.csv",
	} {
		t.Run(name, func(t *testing.T) {
			_, rerr := store.Read(path)
			werr := store.Write(ctx, path, []byte("x"))
			cerr := store.Create(ctx, path, []byte("x"))
			uerr := store.Update(ctx, path, func([]byte, bool) ([]byte, error) { return []byte("x"), nil })

			for _, err := range []error{rerr, werr, cerr, uerr} {
				assert.ErrorIs(t, err, store.ErrInvalidPath)
			}
			_, err := os.Stat(outside)
			assert.True(t, errors.Is(err, fs.ErrNotExist), "nothing was written outside the root")
		})
	}
}

func TestCreate_WritesANewFileAndRefusesAnExistingOne(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()

	require.NoError(t, store.Create(ctx, "n.csv", []byte("first")))
	err := store.Create(ctx, "n.csv", []byte("second"))

	require.ErrorIs(t, err, fs.ErrExist)
	got, rerr := store.Read("n.csv")
	require.NoError(t, rerr)
	assert.Equal(t, "first", string(got), "the existing file is not replaced")
}

func TestCreate_LeavesNoTemporaryFile(t *testing.T) {
	root := storetest.Root(t)
	ctx := context.Background()
	require.NoError(t, store.Create(ctx, "d/n.csv", []byte("x")))
	_ = store.Create(ctx, "d/n.csv", []byte("y"))

	entries, err := os.ReadDir(filepath.Join(root, "d"))

	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestCreate_OfTwoConcurrentCallersExactlyOneWins(t *testing.T) {
	storetest.Root(t)
	var wins, exists atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch err := store.Create(context.Background(), "race.csv", []byte("x")); {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, fs.ErrExist):
				exists.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 1, wins.Load())
	assert.EqualValues(t, 7, exists.Load())
}

func TestUpdate_SeesNoFileThenTheFile(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	var sawExists []bool
	appendTo := func(old []byte, exists bool) ([]byte, error) {
		sawExists = append(sawExists, exists)
		return append(old, 'x'), nil
	}

	require.NoError(t, store.Update(ctx, "u.csv", appendTo))
	require.NoError(t, store.Update(ctx, "u.csv", appendTo))

	got, err := store.Read("u.csv")
	require.NoError(t, err)
	assert.Equal(t, "xx", string(got))
	assert.Equal(t, []bool{false, true}, sawExists)
}

func TestUpdate_ErrUnchangedLeavesTheFileAloneAndIsNotAnError(t *testing.T) {
	root := storetest.Root(t)
	ctx := context.Background()
	require.NoError(t, store.Write(ctx, "u.csv", []byte("keep")))
	before, err := os.Stat(filepath.Join(root, "u.csv"))
	require.NoError(t, err)

	err = store.Update(ctx, "u.csv", func([]byte, bool) ([]byte, error) { return []byte("changed"), store.ErrUnchanged })

	require.NoError(t, err)
	got, rerr := store.Read("u.csv")
	require.NoError(t, rerr)
	assert.Equal(t, "keep", string(got))
	after, serr := os.Stat(filepath.Join(root, "u.csv"))
	require.NoError(t, serr)
	assert.True(t, os.SameFile(before, after), "the file was not replaced")
}

func TestUpdate_ErrUnchangedOnAMissingFileCreatesNothing(t *testing.T) {
	storetest.Root(t)

	err := store.Update(context.Background(), "new.csv", func([]byte, bool) ([]byte, error) { return nil, store.ErrUnchanged })

	require.NoError(t, err)
	_, rerr := store.Read("new.csv")
	assert.ErrorIs(t, rerr, fs.ErrNotExist)
}

func TestUpdate_ErrorFromTheCallbackLeavesTheFileUntouched(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	require.NoError(t, store.Write(ctx, "u.csv", []byte("keep")))
	boom := errors.New("boom")

	err := store.Update(ctx, "u.csv", func([]byte, bool) ([]byte, error) { return nil, boom })

	require.ErrorIs(t, err, boom)
	got, rerr := store.Read("u.csv")
	require.NoError(t, rerr)
	assert.Equal(t, "keep", string(got))
}

func TestUpdate_OverlappingCyclesLoseNothing(t *testing.T) {
	storetest.Root(t)
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := store.Update(context.Background(), "count.csv", func(old []byte, _ bool) ([]byte, error) {
				return append(old, 'x'), nil
			})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	got, err := store.Read("count.csv")
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("x", n), string(got), "every read-modify-write saw the previous one's result")
}

func TestList_ReturnsFilesRecursivelyRelativeAndSorted(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	for _, p := range []string{"b/2.csv", "a/z/1.csv", "a/0.csv", "other/x.csv"} {
		require.NoError(t, store.Write(ctx, p, []byte("x")))
	}

	got, err := store.List("a")
	require.NoError(t, err)
	all, aerr := store.List("")

	assert.Equal(t, []string{filepath.Join("a", "0.csv"), filepath.Join("a", "z", "1.csv")}, got)
	require.NoError(t, aerr)
	assert.Equal(t, []string{
		filepath.Join("a", "0.csv"), filepath.Join("a", "z", "1.csv"),
		filepath.Join("b", "2.csv"), filepath.Join("other", "x.csv"),
	}, all)
}

func TestList_MissingDirectoryIsNotExistAndEscapeIsInvalid(t *testing.T) {
	storetest.Root(t)

	_, err := store.List("missing")
	_, escape := store.List("../x")

	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.ErrorIs(t, escape, store.ErrInvalidPath)
}

// TestUpdate_WaitingForTheLockRespectsCancellation holds a path's lock inside an
// Update callback and checks that a second writer gives up when its context is
// done, rather than blocking forever, and that the first still completes.
func TestUpdate_WaitingForTheLockRespectsCancellation(t *testing.T) {
	storetest.Root(t)
	holding, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.Update(context.Background(), "l.csv", func([]byte, bool) ([]byte, error) {
			close(holding)
			<-release
			return []byte("first"), nil
		})
	}()
	<-holding
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := store.Write(ctx, "l.csv", []byte("second"))

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	close(release)
	require.NoError(t, <-done)
	got, rerr := store.Read("l.csv")
	require.NoError(t, rerr)
	assert.Equal(t, "first", string(got))
}

func TestLocks_DifferentPathsDoNotBlockEachOther(t *testing.T) {
	storetest.Root(t)
	holding, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.Update(context.Background(), "one.csv", func([]byte, bool) ([]byte, error) {
			close(holding)
			<-release
			return []byte("1"), nil
		})
	}()
	<-holding

	err := store.Write(context.Background(), "two.csv", []byte("2"))

	require.NoError(t, err)
	close(release)
	require.NoError(t, <-done)
}

// fileOps are the standard-library calls that touch files. Within the data
// packages only the store may use them; everything else reads and writes the data
// directory through the store.
var fileOps = map[string]map[string]bool{
	"os": {
		"ReadFile": true, "WriteFile": true, "Open": true, "OpenFile": true, "Create": true,
		"CreateTemp": true, "Rename": true, "Remove": true, "RemoveAll": true, "MkdirAll": true,
		"Mkdir": true, "MkdirTemp": true, "ReadDir": true, "Stat": true, "Lstat": true,
		"Link": true, "Symlink": true, "Truncate": true, "Chmod": true,
	},
	"filepath": {"Walk": true, "WalkDir": true, "Glob": true},
	"ioutil":   {"ReadFile": true, "WriteFile": true, "ReadDir": true, "TempFile": true, "TempDir": true},
}

// TestDataPackages_DoFileIOOnlyThroughStore enforces issue #644: the provider,
// market-data, application-service and layout packages (the code that reads and
// writes the data directory) call no file function of the standard library; they
// go through store. Test files
// are exempt, since they lay out fixtures and inspect results directly.
func TestDataPackages_DoFileIOOnlyThroughStore(t *testing.T) {
	checked := 0
	for _, dir := range []string{"../../provider", "../../marketdata", "../../service", "../marketdata"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			checked++
			f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			require.NoError(t, perr)
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && fileOps[pkg.Name][sel.Sel.Name] {
					assert.Failf(t, "file I/O outside the store", "%s calls %s.%s: read and write data files through the store package", path, pkg.Name, sel.Sel.Name)
				}
				return true
			})
			return nil
		})
		require.NoError(t, err)
	}
	assert.Positive(t, checked)
}
