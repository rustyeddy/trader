package canonical

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/store"
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
	assert.Equal(t, "EURUSD", got.Bars[0].Instrument, "Read names the bars from the key")
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

func TestPublish_BuildsThenLeavesAnIdenticalBuildAlone(t *testing.T) {
	root := storetest.Root(t)
	ctx := context.Background()
	f := goodFile(t)

	first, err := Publish(ctx, f)
	require.NoError(t, err)
	path, _ := f.Key.Path()
	before, err := os.Stat(filepath.Join(root, path))
	require.NoError(t, err)
	f.Meta.BuiltAt = f.Meta.BuiltAt.Add(time.Hour) // provenance only: not part of the build
	second, err := Publish(ctx, f)

	require.NoError(t, err)
	assert.Equal(t, Built, first)
	assert.Equal(t, Unchanged, second)
	after, err := os.Stat(filepath.Join(root, path))
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after), "an unchanged build rewrites nothing")
	got, err := Read(ctx, f.Key)
	require.NoError(t, err)
	assert.Equal(t, goodFile(t).Meta.BuiltAt, got.Meta.BuiltAt, "the file keeps its original build time")
}

func TestPublish_ReplacesAfterEachInputToTheBuildChanges(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	for name, mutate := range map[string]func(*Meta){
		"raw fingerprint":   func(m *Meta) { m.RawFingerprint = "sha256:other" },
		"builder version":   func(m *Meta) { m.BuilderVersion = "builder-v2" },
		"validator version": func(m *Meta) { m.ValidatorVersion = "validator-v2" },
		// A policy this validator does not know is not checked bar by bar, so the
		// same bars are valid under it.
		"availability policy": func(m *Meta) { m.AvailabilityPolicy = "scheduled-close-v0" },
	} {
		t.Run(name, func(t *testing.T) {
			storetest.Root(t)
			f := goodFile(t)
			_, err := Publish(ctx, f)
			require.NoError(t, err)
			mutate(&f.Meta)

			got, err := Publish(ctx, f)

			require.NoError(t, err)
			assert.Equal(t, Replaced, got)
			back, err := Read(ctx, f.Key)
			require.NoError(t, err)
			assert.True(t, back.Meta.SameBuild(f.Meta), "the file now records the new build")
		})
	}
}

func TestPublish_ReplacesAFileThatIsNotCanonicalV2(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	f := goodFile(t)
	path, _ := f.Key.Path()
	require.NoError(t, store.Write(ctx, path, []byte("# schema=canonical-v1\nold,format\n")))

	got, err := Publish(ctx, f)

	require.NoError(t, err)
	assert.Equal(t, Replaced, got)
	_, err = Read(ctx, f.Key)
	assert.NoError(t, err, "the replacement reads back")
}

func TestPublish_RejectsAnInvalidFileAndWritesNothing(t *testing.T) {
	storetest.Root(t)
	f := goodFile(t)
	f.Bars[0].AvailableAt = f.Bars[0].Time // not what the policy gives

	_, err := Publish(context.Background(), f)

	require.ErrorIs(t, err, ErrMalformedData)
	_, rerr := Read(context.Background(), f.Key)
	assert.ErrorIs(t, rerr, ErrNotFound)
}

func TestPublish_CancelledLeavesTheFileAsItWas(t *testing.T) {
	storetest.Root(t)
	f := goodFile(t)
	_, err := Publish(context.Background(), f)
	require.NoError(t, err)
	f.Meta.RawFingerprint = "sha256:other"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = Publish(ctx, f)

	require.ErrorIs(t, err, context.Canceled)
	got, rerr := Read(context.Background(), f.Key)
	require.NoError(t, rerr)
	assert.Equal(t, "sha256:abc", got.Meta.RawFingerprint)
}

func TestPublish_OverlappingPublishesOfOneBuildWriteOnce(t *testing.T) {
	storetest.Root(t)
	var wg sync.WaitGroup
	outcomes := make(chan Outcome, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := Publish(context.Background(), goodFile(t))
			assert.NoError(t, err)
			outcomes <- got
		}()
	}
	wg.Wait()
	close(outcomes)

	counts := map[Outcome]int{}
	for o := range outcomes {
		counts[o]++
	}
	assert.Equal(t, 1, counts[Built], "exactly one publisher builds the file")
	assert.Equal(t, 7, counts[Unchanged], "the others find the same build already there")
}
