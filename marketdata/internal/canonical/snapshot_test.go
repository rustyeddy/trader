package canonical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
)

func TestFingerprint(t *testing.T) {
	sum := sha256.Sum256([]byte("abc"))
	assert.Equal(t, "sha256:"+hex.EncodeToString(sum[:]), Fingerprint([]byte("abc")))
}

func TestArchiveSnapshot_KeepsTheBytesAndTheFirstRecord(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	data := []byte("a,b\r\n")
	rec := SnapshotRecord{AsOf: "2026-09-10", File: "d.zip", Member: "m", ArchiveSHA256: "sha256:zz"}

	_, found, err := SnapshotDate("stooq", "SPY", data)
	require.NoError(t, err)
	assert.False(t, found)
	got, path, err := ArchiveSnapshot(ctx, "stooq", "SPY", data, rec)

	require.NoError(t, err)
	hash := Fingerprint(data)[len("sha256:"):]
	assert.Equal(t, "stooq/snapshots/SPY/"+hash+".raw", path)
	rec.Fingerprint = Fingerprint(data)
	assert.Equal(t, rec, got)
	stored, err := store.Read(path)
	require.NoError(t, err)
	assert.Equal(t, data, stored, "unchanged")

	again, _, err := ArchiveSnapshot(ctx, "stooq", "SPY", data, SnapshotRecord{AsOf: "2027-01-01", File: "other"})
	require.NoError(t, err)
	assert.Equal(t, rec, again, "the first record stands")
	same, _, err := ArchiveSnapshot(ctx, "stooq", "SPY", data, SnapshotRecord{})
	require.NoError(t, err, "no date is needed once recorded")
	assert.Equal(t, rec, same)
	date, found, err := SnapshotDate("stooq", "SPY", data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "2026-09-10", date)
}

func TestArchiveSnapshot_NewBytesNeedAValidDateAndWriteNothingWithout(t *testing.T) {
	storetest.Root(t)
	for _, asOf := range []string{"", "10/09/2026"} {
		_, _, err := ArchiveSnapshot(context.Background(), "stooq", "SPY", []byte("x"), SnapshotRecord{AsOf: asOf})
		assert.ErrorIs(t, err, ErrNoSnapshotDate)
	}
	files, err := store.List("")
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestArchiveSnapshot_RefusesAnArchiveThatIsNotWhatItsNameSays(t *testing.T) {
	ctx := context.Background()
	data := []byte("x")
	hash := Fingerprint(data)[len("sha256:"):]
	base := "stooq/snapshots/SPY/" + hash
	rec := SnapshotRecord{AsOf: "2026-09-10"}
	for name, files := range map[string]map[string]string{
		"other bytes":        {base + ".raw": "y"},
		"unreadable record":  {base + ".json": "{"},
		"another's record":   {base + ".json": `{"snapshot_fingerprint":"sha256:00","snapshot_as_of":"2026-09-10"}`},
		"record with no day": {base + ".json": `{"snapshot_fingerprint":"` + Fingerprint(data) + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			storetest.Root(t)
			for p, body := range files {
				require.NoError(t, store.Write(ctx, p, []byte(body)))
			}

			_, _, err := ArchiveSnapshot(ctx, "stooq", "SPY", data, rec)

			assert.ErrorIs(t, err, ErrSnapshot)
		})
	}
	_, _, err := ArchiveSnapshot(ctx, "stooq", "..", data, rec)
	assert.Error(t, err)
	_, _, err = SnapshotDate("", "SPY", data)
	assert.Error(t, err)
}

func TestMonths_ListsOneSeriesInOrder(t *testing.T) {
	storetest.Root(t)
	none, err := Months("stooq", "SPY", D1)
	require.NoError(t, err)
	assert.Empty(t, none)
	for _, p := range []string{
		"stooq/canonical/SPY/2024/02/SPY-2024-02-d1.csv", "stooq/canonical/SPY/2023/12/SPY-2023-12-d1.csv",
		"stooq/canonical/SPY/2024/01/SPY-2024-01-h1.csv", "stooq/canonical/SPY/notes.txt",
		"stooq/canonical/QQQ/2024/01/QQQ-2024-01-d1.csv",
	} {
		require.NoError(t, store.Write(context.Background(), p, []byte("x")))
	}

	got, err := Months("stooq", "SPY", D1)

	require.NoError(t, err)
	assert.Equal(t, []Key{{"stooq", "SPY", D1, 2023, time.December}, {"stooq", "SPY", D1, 2024, time.February}}, got)
	_, err = Months("..", "SPY", D1)
	assert.Error(t, err)
}
