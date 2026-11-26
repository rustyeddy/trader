package store_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// put writes a file under the root with plain os calls (tests lay out fixtures
// directly) and returns its absolute path.
func put(t *testing.T, root, rel, content string) string {
	t.Helper()
	abs := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	return abs
}

func TestInventory_ListsFilesInOrderWithSizeTimeAndInode(t *testing.T) {
	root := storetest.Root(t)
	b := put(t, root, "raw/oanda/B.csv", "bbbb")
	put(t, root, "raw/oanda/A/2024/01/A.csv", "aa")
	when := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, os.Chtimes(b, when, when))

	got, err := store.Inventory(context.Background(), "raw")

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, filepath.Join("raw", "oanda", "A", "2024", "01", "A.csv"), got[0].Path)
	assert.Equal(t, int64(2), got[0].Size)
	assert.Equal(t, filepath.Join("raw", "oanda", "B.csv"), got[1].Path)
	assert.Equal(t, int64(4), got[1].Size)
	assert.True(t, got[1].ModTime.Equal(when))
	assert.False(t, got[1].Symlink)
	if runtime.GOOS == "linux" {
		assert.NotZero(t, got[1].Inode)
	}
}

func TestInventory_RecordsASymlinkAndDoesNotFollowIt(t *testing.T) {
	root := storetest.Root(t)
	put(t, root, "raw/oanda/real.csv", "x")
	require.NoError(t, os.Symlink(filepath.Join(root, "raw/oanda/real.csv"), filepath.Join(root, "raw/oanda/link.csv")))
	require.NoError(t, os.Symlink(root, filepath.Join(root, "raw/loop")))

	got, err := store.Inventory(context.Background(), "raw")

	require.NoError(t, err)
	var links []string
	for _, e := range got {
		if e.Symlink {
			links = append(links, filepath.Base(e.Path))
		}
	}
	assert.ElementsMatch(t, []string{"link.csv", "loop"}, links, "a link to a directory is one entry, not a tree to walk")
	assert.Len(t, got, 3)
}

func TestInventory_RefusesWhatItCannotRead(t *testing.T) {
	storetest.Root(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	_, missing := store.Inventory(context.Background(), "nope")
	_, escaping := store.Inventory(context.Background(), "../x")
	_, empty := store.Inventory(context.Background(), "")
	_, stopped := store.Inventory(cancelled, "nope")

	assert.ErrorIs(t, missing, fs.ErrNotExist)
	assert.ErrorIs(t, escaping, store.ErrInvalidPath)
	assert.ErrorIs(t, empty, store.ErrInvalidPath)
	assert.Error(t, stopped)
}

func TestInventory_StopsWhenCancelledMidWalk(t *testing.T) {
	root := storetest.Root(t)
	for _, n := range []string{"a", "b", "c"} {
		put(t, root, "d/"+n, n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.Inventory(ctx, "d")

	assert.ErrorIs(t, err, context.Canceled)
}

func TestMove_RenamesADirectoryKeepingEveryFile(t *testing.T) {
	root := storetest.Root(t)
	f := put(t, root, "raw/oanda/EURUSD/2024/01/x.csv", "data")
	before, err := os.Stat(f)
	require.NoError(t, err)

	require.NoError(t, store.Move(context.Background(), "raw/oanda", "oanda/raw"))

	moved := filepath.Join(root, "oanda/raw/EURUSD/2024/01/x.csv")
	after, err := os.Stat(moved)
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after), "the same file: not a copy")
	assert.NoFileExists(t, f)
	content, err := store.Read("oanda/raw/EURUSD/2024/01/x.csv")
	require.NoError(t, err)
	assert.Equal(t, "data", string(content))
}

func TestMove_NeverOverwritesAnything(t *testing.T) {
	root := storetest.Root(t)
	put(t, root, "raw/oanda/a.csv", "src")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "oanda/raw"), 0o755)) // an empty directory: rename(2) would replace it
	put(t, root, "other/raw/keep.csv", "keep")

	emptyDest := store.Move(context.Background(), "raw/oanda", "oanda/raw")
	fullDest := store.Move(context.Background(), "raw/oanda", "other/raw")

	assert.ErrorIs(t, emptyDest, store.ErrExists)
	assert.ErrorIs(t, fullDest, store.ErrExists)
	assert.FileExists(t, filepath.Join(root, "raw/oanda/a.csv"), "the source is untouched")
	assert.FileExists(t, filepath.Join(root, "other/raw/keep.csv"))
}

func TestMove_RefusesWhatIsNotAMovableDirectory(t *testing.T) {
	root := storetest.Root(t)
	put(t, root, "raw/file.csv", "x")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	notDir := store.Move(context.Background(), "raw/file.csv", "moved")
	missing := store.Move(context.Background(), "raw/nope", "moved")
	badOld := store.Move(context.Background(), "../raw", "moved")
	badNew := store.Move(context.Background(), "raw", "/abs")
	stopped := store.Move(cancelled, "raw", "moved")

	assert.ErrorContains(t, notDir, "not a directory")
	assert.ErrorIs(t, missing, fs.ErrNotExist)
	assert.ErrorIs(t, badOld, store.ErrInvalidPath)
	assert.ErrorIs(t, badNew, store.ErrInvalidPath)
	assert.ErrorIs(t, stopped, context.Canceled)
	assert.NoDirExists(t, filepath.Join(root, "moved"))
}

func TestExists_DoesNotFollowALink(t *testing.T) {
	root := storetest.Root(t)
	put(t, root, "a/f", "x")
	require.NoError(t, os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "dangling")))

	file, err := store.Exists("a/f")
	require.NoError(t, err)
	dangling, err := store.Exists("dangling")
	require.NoError(t, err)
	absent, err := store.Exists("a/g")
	require.NoError(t, err)
	_, invalid := store.Exists("../x")

	assert.True(t, file)
	assert.True(t, dangling, "a link is there even when it points nowhere")
	assert.False(t, absent)
	assert.ErrorIs(t, invalid, store.ErrInvalidPath)
}

func TestChecksum_IsTheSHA256OfTheBytes(t *testing.T) {
	root := storetest.Root(t)
	put(t, root, "f", "abc")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := store.Checksum(context.Background(), "f")
	_, missing := store.Checksum(context.Background(), "g")
	_, stopped := store.Checksum(cancelled, "f")

	require.NoError(t, err)
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", got)
	assert.ErrorIs(t, missing, fs.ErrNotExist)
	assert.ErrorIs(t, stopped, context.Canceled)
}

func TestSubdirs_ListsOnlyDirectoriesInOrder(t *testing.T) {
	root := storetest.Root(t)
	put(t, root, "raw/b/x", "x")
	put(t, root, "raw/a/x", "x")
	put(t, root, "raw/file.csv", "x")
	require.NoError(t, os.Symlink(root, filepath.Join(root, "raw/link")))

	got, err := store.Subdirs("raw")
	top, terr := store.Subdirs("")
	_, missing := store.Subdirs("nope")
	_, invalid := store.Subdirs("../x")

	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got, "no files, no links")
	require.NoError(t, terr)
	assert.Equal(t, []string{"raw"}, top, "the root itself")
	assert.ErrorIs(t, missing, fs.ErrNotExist)
	assert.ErrorIs(t, invalid, store.ErrInvalidPath)
}

func TestIsTemp_RecognisesTheStoresOwnTemporaryFiles(t *testing.T) {
	assert.True(t, store.IsTemp(".EURUSD-2024-01-h1.csv.tmp-123456"))
	assert.True(t, store.IsTemp("raw/oanda/.x.csv.tmp-9"))
	assert.False(t, store.IsTemp("EURUSD-2024-01-h1.csv"))
	assert.False(t, store.IsTemp("x.tmp-1"), "a visible file is not the store's")
	assert.False(t, store.IsTemp(""))
}

func TestOpen_ReadsTheFileAndReportsAMissingOrOutsidePath(t *testing.T) {
	storetest.Root(t)
	require.NoError(t, store.Write(context.Background(), "a/b.zip", []byte("0123")))

	f, err := store.Open("a/b.zip")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	got := make([]byte, 2)
	_, err = f.ReadAt(got, 2)
	require.NoError(t, err)
	assert.Equal(t, "23", string(got))
	_, err = f.Write([]byte("x"))
	assert.Error(t, err, "opened for reading only")

	_, err = store.Open("a/none")
	assert.ErrorIs(t, err, fs.ErrNotExist)
	_, err = store.Open("../x")
	assert.ErrorIs(t, err, store.ErrInvalidPath)
}
