package main

import (
	"bytes"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// silenceLogs keeps the run's slog diagnostics out of the test output.
func silenceLogs(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func TestNewRootCmd_PrintsEveryScenarioSummaryToStdout(t *testing.T) {
	silenceLogs(t)
	t.Chdir("../..") // scenario data paths are relative to the repository root

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetArgs(nil)

	require.NoError(t, cmd.Execute())

	for _, scenario := range []string{
		"a-reversal-exit", "b1-stop-wick", "b2-stop-cross",
		"b3-stop-gap", "c-reversal", "d-short-stop",
	} {
		assert.Contains(t, out.String(), scenario+":")
	}
}

func TestNewRootCmd_RejectsPositionalArguments(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"extra"})

	require.Error(t, cmd.Execute())
	assert.Empty(t, out.String(), "nothing runs when the arguments are invalid")
}

func TestNewRootCmd_LeavesErrorPrintingToMain(t *testing.T) {
	cmd := newRootCmd()

	assert.True(t, cmd.SilenceErrors)
	assert.True(t, cmd.SilenceUsage)
}
