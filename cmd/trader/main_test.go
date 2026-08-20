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

func TestRun_ErrorReturnsOne(t *testing.T) {
	// data (like root) is deliberately non-runnable in #108's own scope
	// (no market-data commands yet), so an unrecognized flag -- which
	// fails during Cobra's own flag parsing, before Runnable() is even
	// checked -- is the one real, always-reachable failure mode run()
	// has today. See root_test.go's newProbeCmd doc comment for why a
	// PersistentPreRunE-level failure (an invalid --log-format, say)
	// cannot be reached through the real, current command tree yet.
	var code int
	withArgs(t, []string{"--this-flag-does-not-exist"}, func() {
		code = run()
	})
	require.Equal(t, 1, code)
}
