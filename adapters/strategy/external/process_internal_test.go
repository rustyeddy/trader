package external

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/logging"
)

// TestLineLogger_BuffersAndLogsCompleteLines exercises lineLogger
// directly: partial writes across multiple Write calls, several
// complete lines in one call, and a trailing partial line only
// surfaced via Flush.
func TestLineLogger_BuffersAndLogsCompleteLines(t *testing.T) {
	logger, rec := logging.Capture()
	w := newLineLogger(logger, "test stderr")

	n, err := w.Write([]byte("first pa"))
	require.NoError(t, err)
	require.Equal(t, 8, n)
	require.Empty(t, rec.Records())

	_, err = w.Write([]byte("rt\nsecond\nthird part"))
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 2)
	require.Equal(t, "first part", records[0].Attrs["line"])
	require.Equal(t, "second", records[1].Attrs["line"])

	require.NoError(t, w.Flush())
	records = rec.Records()
	require.Len(t, records, 3)
	require.Equal(t, "third part", records[2].Attrs["line"])

	// Flush with nothing buffered is a no-op, not an extra record.
	require.NoError(t, w.Flush())
	require.Len(t, rec.Records(), 3)
}

// TestLineLogger_EmptyLinesAreNotLogged proves a blank line (for
// example a stray trailing newline) does not produce a noise record.
func TestLineLogger_EmptyLinesAreNotLogged(t *testing.T) {
	logger, rec := logging.Capture()
	w := newLineLogger(logger, "test stderr")

	_, err := w.Write([]byte("\n\nreal line\n"))
	require.NoError(t, err)

	records := rec.Records()
	require.Len(t, records, 1)
	require.Equal(t, "real line", records[0].Attrs["line"])
}

func TestTrimTrailingNewline(t *testing.T) {
	require.Equal(t, "abc", trimTrailingNewline("abc\n"))
	require.Equal(t, "abc", trimTrailingNewline("abc\r\n"))
	require.Equal(t, "abc", trimTrailingNewline("abc"))
}

// TestClearStaleSocket_NonRefusalDialErrorFailsClosed is the review's
// own follow-up finding: a failed dial alone is not proof a socket is
// stale. Only ECONNREFUSED (and ENOENT) authorize removal; every other
// dial failure — here, a permission error deliberately injected via
// chmod — must leave the path untouched rather than risk unlinking a
// socket this check could not actually verify.
func TestClearStaleSocket_NonRefusalDialErrorFailsClosed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permission checks are bypassed, so this test cannot inject a non-ECONNREFUSED dial failure")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.sock")

	stale, err := net.Listen("unix", path)
	require.NoError(t, err)
	stale.(*net.UnixListener).SetUnlinkOnClose(false) // leave the file behind once closed
	require.NoError(t, stale.Close())

	require.NoError(t, os.Chmod(path, 0o000)) // dial now fails with EACCES, not ECONNREFUSED

	err = clearStaleSocket(path)
	require.Error(t, err)

	_, statErr := os.Lstat(path)
	require.NoError(t, statErr, "the socket file must not have been removed on an unclassifiable dial error")

	require.NoError(t, os.Chmod(path, 0o600)) // restore so t.TempDir's own cleanup can remove it
}

// TestClearStaleSocket_ECONNREFUSEDIsRemoved proves the positive case
// still works: a genuinely stale socket (dial refused) is removed.
func TestClearStaleSocket_ECONNREFUSEDIsRemoved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sock")

	stale, err := net.Listen("unix", path)
	require.NoError(t, err)
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, stale.Close())

	require.NoError(t, clearStaleSocket(path))

	_, statErr := os.Lstat(path)
	require.True(t, os.IsNotExist(statErr), "a genuinely stale socket should have been removed")
}

// TestClearStaleSocket_MissingPathIsANoOp proves a path that does not
// exist at all requires no action and reports no error.
func TestClearStaleSocket_MissingPathIsANoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.sock")
	require.NoError(t, clearStaleSocket(path))
}
