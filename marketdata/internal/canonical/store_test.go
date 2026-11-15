package canonical

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteRead_RoundTripsThroughTheLayoutPath(t *testing.T) {
	root := storetest.Root(t)
	f := goodFile(t)
	f.Bad = []BadRecord{{Time: utc(2024, 1, 2, 9), Reason: ReasonCrossed, RawRow: 4}}

	require.NoError(t, Write(context.Background(), f))
	got, err := Read(context.Background(), f.Key)

	require.NoError(t, err)
	assert.Len(t, got.Bars, 3)
	assert.Len(t, got.Bad, 1)
	assert.False(t, got.Clean())
	path, _ := f.Key.Path()
	path = filepath.Join(root, path)
	assert.FileExists(t, path)
}

func TestWrite_ReplacesAnExistingFileAndLeavesNoTemporaryFile(t *testing.T) {
	root := storetest.Root(t)
	f := goodFile(t)
	require.NoError(t, Write(context.Background(), f))
	f.Bars = f.Bars[:1]
	f.Meta.RawFingerprint = "sha256:changed"

	require.NoError(t, Write(context.Background(), f))

	got, err := Read(context.Background(), f.Key)
	require.NoError(t, err)
	assert.Len(t, got.Bars, 1)
	assert.Equal(t, "sha256:changed", got.Meta.RawFingerprint)
	path, _ := f.Key.Path()
	path = filepath.Join(root, path)
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "only the file itself remains")
}

func TestWrite_RefusesAnInvalidFileBeforeCreatingAnything(t *testing.T) {
	root := storetest.Root(t)
	f := goodFile(t)
	f.Bars[0].Volume = -1

	err := Write(context.Background(), f)

	assert.ErrorIs(t, err, ErrMalformedData)
	entries, rerr := os.ReadDir(root)
	require.NoError(t, rerr)
	assert.Empty(t, entries)

	f = goodFile(t)
	f.Key.Symbol = "../escape"
	assert.Error(t, Write(context.Background(), f))
}

func TestWrite_CancelledBeforeTheCommitKeepsTheExistingFile(t *testing.T) {
	root := storetest.Root(t)
	f := goodFile(t)
	require.NoError(t, Write(context.Background(), f))
	path, _ := f.Key.Path()
	path = filepath.Join(root, path)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.Bars = f.Bars[:1]

	assert.ErrorIs(t, Write(ctx, f), context.Canceled)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}

func TestWrite_ReportsAnUnwritableDirectory(t *testing.T) {
	root := storetest.Root(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "oanda"), []byte("a file, not a directory"), 0o644))

	err := Write(context.Background(), goodFile(t))

	require.Error(t, err)
}

func TestRead_AMissingPartitionNamesThePathLookedUp(t *testing.T) {
	root := storetest.Root(t)

	_, err := Read(context.Background(), jan24H1())

	require.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	path, _ := jan24H1().Path()
	path = filepath.Join(root, path)
	assert.Contains(t, err.Error(), path)
}

func TestRead_ThePathIsAuthoritative(t *testing.T) {
	root := storetest.Root(t)
	f := goodFile(t)
	require.NoError(t, Write(context.Background(), f))
	// Move the January file to where February's would be.
	feb := f.Key
	feb.Month = 2
	from, _ := f.Key.Path()
	from = filepath.Join(root, from)
	to, _ := feb.Path()
	to = filepath.Join(root, to)
	require.NoError(t, os.MkdirAll(filepath.Dir(to), 0o755))
	require.NoError(t, os.Rename(from, to))

	_, err := Read(context.Background(), feb)

	require.ErrorIs(t, err, ErrMalformedData)
	assert.Contains(t, err.Error(), "path names")
}

func TestRead_RefusesBadInput(t *testing.T) {
	root := storetest.Root(t)
	path, _ := jan24H1().Path()
	path = filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("not a canonical file\n"), 0o644))

	_, err := Read(context.Background(), jan24H1())
	assert.ErrorIs(t, err, ErrMalformedData)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Read(ctx, jan24H1())
	assert.ErrorIs(t, err, context.Canceled)

	bad := jan24H1()
	bad.Interval = "w1"
	_, err = Read(context.Background(), bad)
	assert.ErrorIs(t, err, ErrUnsupportedInterval)
}

func TestRead_ReportsAnUnreadablePath(t *testing.T) {
	root := storetest.Root(t)
	path, _ := jan24H1().Path()
	path = filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(path, 0o755)) // a directory where the file should be

	_, err := Read(context.Background(), jan24H1())

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
}

func TestWrite_ReportsARenameFailureAndCleansUp(t *testing.T) {
	root := storetest.Root(t)
	f := goodFile(t)
	path, _ := f.Key.Path()
	path = filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(filepath.Join(path, "keep"), 0o755)) // a non-empty directory where the file goes

	err := Write(context.Background(), f)

	require.Error(t, err)
	entries, rerr := os.ReadDir(filepath.Dir(path))
	require.NoError(t, rerr)
	assert.Len(t, entries, 1, "the temporary file is removed")
}
