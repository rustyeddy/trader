package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyTree lays out an archive in the old layout under a fresh root: raw for
// three sources (one with hour files and a partial download, as dukascopy has) and
// a canonical-v1 tree that must never move. It returns the root.
func legacyTree(t *testing.T) string {
	t.Helper()
	root := storetest.Root(t)
	files := map[string]string{
		"raw/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv":       "# schema=raw-v1\n1\n",
		"raw/oanda/EURUSD/2024/02/EURUSD-2024-02-h1.csv":       "# schema=raw-v1\n22\n",
		"raw/oanda/GBPUSD/2024/01/GBPUSD-2024-01-d1.csv":       "# schema=raw-v1\n333\n",
		"raw/dukascopy/EURUSD/2026/03/08/22.bi5":               "tick-bytes",
		"raw/dukascopy/EURUSD/2026/03/09/00.bi5.part":          "half",
		"raw/stooq-managed/spy.us.csv":                         "date,close\n",
		"canonical/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv": "# schema=canonical-v1\n",
		"candles/oanda/old.csv":                                "not ours",
	}
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	}
	return root
}

func listing(t *testing.T) []string {
	t.Helper()
	files, err := store.List("")
	require.NoError(t, err)
	return files
}

func problems(r MigrationReport) []string {
	out := append([]string{}, r.Problems...)
	for _, u := range r.Units {
		out = append(out, u.Problems...)
	}
	return out
}

func TestMigrateLayout_ADryRunPlansAndChangesNothing(t *testing.T) {
	legacyTree(t)
	before := listing(t)

	got, err := MigrateLayout(context.Background(), nil, MigrateRequest{})

	require.NoError(t, err)
	assert.True(t, got.OK)
	assert.False(t, got.Applied)
	require.Len(t, got.Units, 3)
	assert.Equal(t, []string{"dukascopy", "oanda", "stooq-managed"}, []string{got.Units[0].Source, got.Units[1].Source, got.Units[2].Source})
	oanda := got.Units[1]
	assert.Equal(t, filepath.Join("raw", "oanda"), oanda.From)
	assert.Equal(t, filepath.Join("oanda", "raw"), oanda.To)
	assert.Equal(t, 3, oanda.Files)
	assert.Equal(t, int64(len("# schema=raw-v1\n1\n")+len("# schema=raw-v1\n22\n")+len("# schema=raw-v1\n333\n")), oanda.Bytes)
	assert.Equal(t, MigratePlanned, oanda.State)
	assert.Equal(t, []string{"oanda"}, got.LegacyCanonical, "canonical-v1 is listed, to be rebuilt")
	assert.Equal(t, before, listing(t), "a dry run moves nothing")
	assert.Len(t, got.Units[0].Notes, 1, "the partial download is noted, not refused")
}

func TestMigrateLayout_ApplyMovesEachSourceWholeAndVerifiesIt(t *testing.T) {
	root := legacyTree(t)
	before, err := os.Stat(filepath.Join(root, "raw/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv"))
	require.NoError(t, err)

	got, err := MigrateLayout(context.Background(), nil, MigrateRequest{Apply: true})

	require.NoError(t, err)
	assert.True(t, got.OK)
	for _, u := range got.Units {
		assert.Equal(t, MigrateVerified, u.State, u.Source)
	}
	after, err := os.Stat(filepath.Join(root, "oanda/raw/EURUSD/2024/01/EURUSD-2024-01-h1.csv"))
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after), "the file itself moved: not a copy")
	assert.FileExists(t, filepath.Join(root, "dukascopy/raw/EURUSD/2026/03/08/22.bi5"))
	assert.FileExists(t, filepath.Join(root, "stooq-managed/raw/spy.us.csv"))
	assert.NoDirExists(t, filepath.Join(root, "raw/oanda"))
	assert.FileExists(t, filepath.Join(root, "canonical/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv"), "canonical is never touched")
	assert.FileExists(t, filepath.Join(root, "candles/oanda/old.csv"), "neither is anything else")
}

func TestMigrateLayout_AChecksummedApplyProvesTheBytes(t *testing.T) {
	legacyTree(t)

	got, err := MigrateLayout(context.Background(), nil, MigrateRequest{Apply: true, Checksum: true})

	require.NoError(t, err)
	assert.True(t, got.OK)
	assert.Equal(t, MigrateVerified, got.Units[1].State)
}

func TestMigrateLayout_ReverseRestoresTheOriginalTree(t *testing.T) {
	root := legacyTree(t)
	before := listing(t)
	orig, err := os.Stat(filepath.Join(root, "raw/oanda/GBPUSD/2024/01/GBPUSD-2024-01-d1.csv"))
	require.NoError(t, err)
	_, err = MigrateLayout(context.Background(), nil, MigrateRequest{Apply: true})
	require.NoError(t, err)
	require.NotEqual(t, before, listing(t))

	got, err := MigrateLayout(context.Background(), nil, MigrateRequest{Apply: true, Reverse: true, Checksum: true})

	require.NoError(t, err)
	assert.True(t, got.Reverse)
	assert.Equal(t, before, listing(t), "every file is back where it was")
	back, err := os.Stat(filepath.Join(root, "raw/oanda/GBPUSD/2024/01/GBPUSD-2024-01-d1.csv"))
	require.NoError(t, err)
	assert.True(t, os.SameFile(orig, back))
	assert.Empty(t, got.LegacyCanonical, "a reverse reports no canonical")
}

func TestMigrateLayout_AReverseDryRunFindsOnlySourcesThatHaveARawDirectory(t *testing.T) {
	root := legacyTree(t)
	_, err := MigrateLayout(context.Background(), nil, MigrateRequest{Apply: true})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "oanda/canonical"), 0o755)) // a v2 build, not ours to move

	got, err := MigrateLayout(context.Background(), nil, MigrateRequest{Reverse: true})

	require.NoError(t, err)
	var sources []string
	for _, u := range got.Units {
		sources = append(sources, u.Source)
	}
	assert.Equal(t, []string{"dukascopy", "oanda", "stooq-managed"}, sources, "no candles, no canonical, no tmp")
}

func TestMigrateLayout_ADestinationThatExistsStopsEverything(t *testing.T) {
	root := legacyTree(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "oanda/raw"), 0o755)) // even an empty one
	before := listing(t)

	got, err := MigrateLayout(context.Background(), nil, MigrateRequest{Apply: true})

	require.ErrorIs(t, err, ErrMigrationFailed)
	assert.False(t, got.OK)
	assert.Contains(t, problems(got)[0], "oanda/raw exists")
	assert.Equal(t, before, listing(t), "no unit moved, not even the ones without a problem")
	assert.FileExists(t, filepath.Join(root, "raw/dukascopy/EURUSD/2026/03/08/22.bi5"))
	assert.Equal(t, MigratePlanned, got.Units[0].State)
}

func TestMigrateLayout_ASymlinkOrATemporaryFileStopsEverything(t *testing.T) {
	for name, plant := range map[string]func(root string){
		"symlink": func(root string) {
			require.NoError(t, os.Symlink(root, filepath.Join(root, "raw/oanda/loop")))
		},
		"temp file": func(root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "raw/oanda/EURUSD/2024/01/.EURUSD-2024-01-h1.csv.tmp-42"), []byte("x"), 0o644))
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := legacyTree(t)
			plant(root)
			before := listing(t)

			got, err := MigrateLayout(context.Background(), nil, MigrateRequest{Apply: true})

			require.ErrorIs(t, err, ErrMigrationFailed)
			assert.Len(t, problems(got), 1)
			assert.Equal(t, before, listing(t))
		})
	}
}

func TestMigrateLayout_ANameThatCannotBeASourceIsAProblem(t *testing.T) {
	root := legacyTree(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "raw/has space"), 0o755))
	before := listing(t)

	got, err := MigrateLayout(context.Background(), nil, MigrateRequest{Apply: true})

	require.ErrorIs(t, err, ErrMigrationFailed)
	require.Len(t, got.Problems, 1)
	assert.Contains(t, got.Problems[0], "has space")
	assert.Equal(t, before, listing(t))
}

func TestMigrateLayout_NothingToMigrateIsFine(t *testing.T) {
	storetest.Root(t)

	got, err := MigrateLayout(context.Background(), nil, MigrateRequest{Apply: true})

	require.NoError(t, err)
	assert.True(t, got.OK)
	assert.Empty(t, got.Units)
}

func TestMigrateLayout_ARerunAfterAnInterruptionMovesWhatIsLeft(t *testing.T) {
	root := legacyTree(t)
	require.NoError(t, store.Move(context.Background(), "raw/oanda", "oanda/raw")) // what a first run did before stopping

	got, err := MigrateLayout(context.Background(), nil, MigrateRequest{Apply: true})

	require.NoError(t, err)
	var sources []string
	for _, u := range got.Units {
		sources = append(sources, u.Source)
		assert.Equal(t, MigrateVerified, u.State)
	}
	assert.Equal(t, []string{"dukascopy", "stooq-managed"}, sources)
	assert.FileExists(t, filepath.Join(root, "oanda/raw/GBPUSD/2024/01/GBPUSD-2024-01-d1.csv"))
	assert.FileExists(t, filepath.Join(root, "dukascopy/raw/EURUSD/2026/03/08/22.bi5"))
}

func TestMigrateLayout_RefusesWhatItCannotRun(t *testing.T) {
	legacyTree(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	_, checksumDry := MigrateLayout(context.Background(), nil, MigrateRequest{Checksum: true})
	_, stopped := MigrateLayout(cancelled, nil, MigrateRequest{Apply: true})

	assert.ErrorContains(t, checksumDry, "--checksum needs --apply")
	assert.ErrorIs(t, stopped, context.Canceled)
	assert.FileExists(t, filepath.Join(store.Root(), "raw/oanda/EURUSD/2024/01/EURUSD-2024-01-h1.csv"))
}

// afterMove is a unit moved by hand, with the inventory taken before, to feed
// verifyMoved a changed result.
func afterMove(t *testing.T, root string) (*MigrationUnit, []store.Entry, map[string]string) {
	t.Helper()
	u := &MigrationUnit{From: "raw/oanda", To: "oanda/raw"}
	before, err := store.Inventory(context.Background(), u.From)
	require.NoError(t, err)
	sums, err := checksums(context.Background(), before)
	require.NoError(t, err)
	require.NoError(t, store.Move(context.Background(), u.From, u.To))
	return u, before, sums
}

func TestVerifyMoved_AcceptsTheSameFiles(t *testing.T) {
	root := legacyTree(t)
	u, before, sums := afterMove(t, root)

	got, err := verifyMoved(context.Background(), u, before, sums, true)

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestVerifyMoved_FindsEveryKindOfDifference(t *testing.T) {
	file := "oanda/raw/EURUSD/2024/01/EURUSD-2024-01-h1.csv"
	tests := []struct {
		name     string
		damage   func(t *testing.T, root string)
		checksum bool
		want     string
	}{
		{"a missing file", func(t *testing.T, root string) { require.NoError(t, os.Remove(filepath.Join(root, file))) }, false, "is missing after the move"},
		{"a changed size", func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, file), []byte("longer than before, certainly"), 0o644))
		}, false, "size"},
		{"an extra file", func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, "oanda/raw/extra.csv"), []byte("x"), 0o644))
		}, false, "appeared that was not in the source"},
		{"a changed modification time", func(t *testing.T, root string) {
			later := time.Now().Add(48 * time.Hour)
			require.NoError(t, os.Chtimes(filepath.Join(root, file), later, later))
		}, false, "modification time changed"},
		{"the same file with other bytes, which only the checksum sees", func(t *testing.T, root string) {
			p := filepath.Join(root, file)
			info, err := os.Stat(p)
			require.NoError(t, err)
			f, err := os.OpenFile(p, os.O_WRONLY, 0)
			require.NoError(t, err)
			_, err = f.WriteAt([]byte("X"), 2) // in place: same inode, size; time restored below
			require.NoError(t, err)
			require.NoError(t, f.Close())
			require.NoError(t, os.Chtimes(p, info.ModTime(), info.ModTime()))
		}, true, "SHA-256"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := legacyTree(t)
			u, before, sums := afterMove(t, root)
			tc.damage(t, root)

			got, err := verifyMoved(context.Background(), u, before, sums, tc.checksum)

			require.NoError(t, err)
			require.NotEmpty(t, got)
			assert.Contains(t, strings.Join(got, "\n"), tc.want)
		})
	}
}

func TestVerifyMoved_WithoutAChecksumTheInPlaceEditIsNotSeen(t *testing.T) {
	// The honest limit of the default check, so nobody assumes more of it: a rename
	// cannot change a file, so the inode, size and time are enough to show nothing
	// but a rename happened; --checksum is for the doubter.
	root := legacyTree(t)
	u, before, sums := afterMove(t, root)
	p := filepath.Join(root, "oanda/raw/EURUSD/2024/01/EURUSD-2024-01-h1.csv")
	info, err := os.Stat(p)
	require.NoError(t, err)
	f, err := os.OpenFile(p, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteAt([]byte("X"), 2)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Chtimes(p, info.ModTime(), info.ModTime()))

	plain, err := verifyMoved(context.Background(), u, before, sums, false)
	summed, serr := verifyMoved(context.Background(), u, before, sums, true)

	require.NoError(t, err)
	require.NoError(t, serr)
	assert.Empty(t, plain)
	assert.NotEmpty(t, summed)
}

func TestVerifyMoved_ListsAtMostAFewDifferences(t *testing.T) {
	root := legacyTree(t)
	for i := range maxProblems + 5 {
		require.NoError(t, os.WriteFile(filepath.Join(root, "raw/oanda", "pad"+string(rune('a'+i%26))+string(rune('a'+i/26))+".csv"), []byte("x"), 0o644))
	}
	u, before, sums := afterMove(t, root)
	for i := range maxProblems + 5 {
		require.NoError(t, os.Remove(filepath.Join(root, "oanda/raw", "pad"+string(rune('a'+i%26))+string(rune('a'+i/26))+".csv")))
	}

	got, err := verifyMoved(context.Background(), u, before, sums, false)

	require.NoError(t, err)
	assert.Len(t, got, maxProblems)
}

func TestMoved_MapsAPathIntoTheDestination(t *testing.T) {
	u := &MigrationUnit{From: "raw/oanda", To: "oanda/raw"}

	assert.Equal(t, filepath.Join("oanda", "raw", "EURUSD", "x.csv"), moved(u, filepath.Join("raw", "oanda", "EURUSD", "x.csv")))
}
