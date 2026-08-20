package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// withArgs temporarily replaces os.Args for the duration of fn, restoring
// the original afterward. run() (via signal.NotifyContext and Cobra's
// default os.Args[1:] parsing) is main's own real entry point, so this
// is the only way to exercise its exit-code mapping without actually
// forking a process.
func withArgs(t *testing.T, args []string, fn func()) {
	t.Helper()
	original := os.Args
	os.Args = append([]string{"trader"}, args...)
	defer func() { os.Args = original }()
	fn()
}

func TestRun_SuccessReturnsZero(t *testing.T) {
	var code int
	withArgs(t, []string{"data", "--help"}, func() {
		code = run()
	})
	require.Equal(t, 0, code)
}

func TestRun_ErrorReturnsOneOnFlagParseFailure(t *testing.T) {
	// An unrecognized flag fails during Cobra's own flag parsing,
	// before PersistentPreRunE (or even the Runnable() check) runs.
	var code int
	withArgs(t, []string{"--this-flag-does-not-exist"}, func() {
		code = run()
	})
	require.Equal(t, 1, code)
}

func TestRun_ErrorReturnsOneOnInvalidLogFormat(t *testing.T) {
	// Both root and data carry an explicit RunE (root.go's own doc
	// comment explains why), so PersistentPreRunE's logging-config
	// validation is reachable through a real "trader data" invocation,
	// not only via a test-only probe command.
	var code int
	withArgs(t, []string{"--log-format=bogus", "data"}, func() {
		code = run()
	})
	require.Equal(t, 1, code)
}
