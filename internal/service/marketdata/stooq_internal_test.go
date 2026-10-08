package marketdata

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCtxReaderStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := ctxReader{ctx: ctx, r: strings.NewReader("abcdef")}

	buf := make([]byte, 3)
	n, err := r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "abc", string(buf[:n]))

	cancel()
	n, err = r.Read(buf)
	assert.Zero(t, n)
	assert.ErrorIs(t, err, context.Canceled)
	_, err = io.Copy(io.Discard, r)
	assert.ErrorIs(t, err, context.Canceled)
}

// writeBundleZIP writes a ZIP whose members are added in the given order.
func writeBundleZIP(t *testing.T, path string, names ...string) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	for _, name := range names {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte("x"))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}

// cancelAfter is a context whose Err reports cancellation once it has been
// consulted more than remaining times, so a test can cancel at every point a
// function checks its context.
type cancelAfter struct {
	context.Context
	remaining, calls int
}

func (c *cancelAfter) Err() error {
	c.calls++
	if c.calls > c.remaining {
		return context.Canceled
	}
	return nil
}

func TestZipHasDailyMember_StopsOnCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d_us_txt.zip")
	writeBundleZIP(t, path, "data/daily/us/nyse etfs/2/spy.us.txt")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	has, err := zipHasDailyMember(ctx, path, "spy")
	require.ErrorIs(t, err, context.Canceled, "cancellation is noticed inside the member loop, even before the matching member")
	assert.False(t, has)

	has, err = zipHasDailyMember(context.Background(), path, "spy")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestZipHasDailyMember_UnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.zip")
	require.NoError(t, os.WriteFile(path, []byte("not a zip"), 0o644))

	has, err := zipHasDailyMember(context.Background(), path, "spy")
	require.NoError(t, err, "a file that is not a ZIP is skipped, not fatal")
	assert.False(t, has)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = zipHasDailyMember(ctx, path, "spy")
	require.ErrorIs(t, err, context.Canceled, "but a canceled context still wins")
}

// Cancelling at every point FindStooqArchive consults its context must give
// context.Canceled and never a wrong archive, and the sweep must reach past
// the directory walk into the ZIP-member scan.
func TestFindStooqArchive_CancellationAtEveryCheckpoint(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "d_us_txt.zip")
	writeBundleZIP(t, bundle,
		"data/daily/us/nasdaq etfs/aaa.us.txt",
		"data/daily/us/nasdaq etfs/bbb.us.txt",
		"data/daily/us/nyse etfs/2/spy.us.txt")

	// A walk over root and the one ZIP, plus the post-walk check, is 3 checks.
	const walkChecks = 3
	checks := -1
	for remaining := 0; remaining < 50; remaining++ {
		ctx := &cancelAfter{Context: context.Background(), remaining: remaining}
		got, err := FindStooqArchive(ctx, root, "SPY")
		if err == nil {
			assert.Equal(t, bundle, got)
			checks = remaining
			break
		}
		require.ErrorIs(t, err, context.Canceled, "remaining=%d", remaining)
	}
	require.NotEqual(t, -1, checks, "FindStooqArchive never completed")
	assert.Greater(t, checks, walkChecks, "the ZIP-member scan consults the context, so cancelling it mid-scan is covered")
}
