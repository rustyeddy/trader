package stooq

import (
	"archive/zip"
	"bytes"
	"context"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/store"
	"github.com/rustyeddy/trader/internal/store/storetest"
)

func put(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, store.Write(context.Background(), path, data))
}

func zipOf(t *testing.T, members ...[2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, m := range members {
		f, err := w.Create(m[0])
		require.NoError(t, err)
		_, err = f.Write([]byte(m[1]))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func TestReadSnapshot_ReadsAMemberUnchangedAndHashesTheArchive(t *testing.T) {
	storetest.Root(t)
	body := head + line("20240301")
	archive := zipOf(t, [2]string{"data/spy.us.txt", body}, [2]string{"data/other.txt", "x"})
	put(t, "stooq/raw/d.zip", archive)

	got, err := ReadSnapshot(context.Background(), "stooq/raw/d.zip", "data/spy.us.txt")

	require.NoError(t, err)
	assert.Equal(t, Snapshot{Data: []byte(body), File: "d.zip", Member: "data/spy.us.txt", ArchiveSHA256: sum(string(archive))}, got)
}

func TestReadSnapshot_ReadsAnExtractedFileWhole(t *testing.T) {
	storetest.Root(t)
	put(t, "stooq/raw/spy.us.txt", []byte("bytes\r\n"))

	got, err := ReadSnapshot(context.Background(), "stooq/raw/spy.us.txt", "")

	require.NoError(t, err)
	assert.Equal(t, Snapshot{Data: []byte("bytes\r\n"), File: "spy.us.txt"}, got)
}

func TestReadSnapshot_Errors(t *testing.T) {
	storetest.Root(t)
	put(t, "stooq/raw/d.zip", zipOf(t, [2]string{"a", "1"}, [2]string{"twice", "1"}, [2]string{"twice", "2"}))
	put(t, "stooq/raw/not.zip", []byte("plain"))
	put(t, "stooq/raw/big.txt", make([]byte, maxSnapshot+1))
	ctx := context.Background()
	for name, tc := range map[string]struct {
		file, member string
		want         error
	}{
		"zip without a member":     {"stooq/raw/d.zip", "", ErrInput},
		"member of a plain file":   {"stooq/raw/big.txt", "a", ErrInput},
		"no such member":           {"stooq/raw/d.zip", "b", ErrInput},
		"two members of that name": {"stooq/raw/d.zip", "twice", ErrInput},
		"not a zip":                {"stooq/raw/not.zip", "a", ErrInput},
		"too large":                {"stooq/raw/big.txt", "", ErrInput},
		"missing":                  {"stooq/raw/none.txt", "", fs.ErrNotExist},
		"outside the root":         {"../etc/passwd", "", store.ErrInvalidPath},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ReadSnapshot(ctx, tc.file, tc.member)
			assert.ErrorIs(t, err, tc.want)
		})
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err := ReadSnapshot(cancelled, "stooq/raw/d.zip", "a")
	assert.ErrorIs(t, err, context.Canceled)
}
