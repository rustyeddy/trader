package external

import (
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
