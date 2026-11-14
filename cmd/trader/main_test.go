package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
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
	a := newApp()
	a.cmd.SetOut(&out)
	a.cmd.SetArgs(nil)

	require.NoError(t, a.execute())

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

func TestNewRootCmd_LogsToTheConfiguredFileAsJSON(t *testing.T) {
	silenceLogs(t)
	t.Chdir("../..")
	path := filepath.Join(t.TempDir(), "trader.log")

	a := newApp()
	a.cmd.SetOut(io.Discard)
	a.cmd.SetArgs([]string{"--log-level", "DEBUG", "--log-format", "json", "--log-output", path})
	require.NoError(t, a.execute())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.NotEmpty(t, lines, "the run logged to the file")
	var first map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first), "one JSON record per line")
	assert.Equal(t, "backtest starting", first["msg"])
}

func TestNewRootCmd_LogLevelFiltersRecords(t *testing.T) {
	silenceLogs(t)
	t.Chdir("../..")
	path := filepath.Join(t.TempDir(), "trader.log")

	a := newApp()
	a.cmd.SetOut(io.Discard)
	a.cmd.SetArgs([]string{"--log-level", "ERROR", "--log-output", path})
	require.NoError(t, a.execute())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(data)), "a run with no errors logs nothing at ERROR")
}

func TestNewRootCmd_RefusesAnUnknownLogLevelOrFormat(t *testing.T) {
	silenceLogs(t)
	for name, args := range map[string][]string{
		"level":  {"--log-level", "LOUD"},
		"format": {"--log-format", "xml"},
		"output": {"--log-output", filepath.Join(t.TempDir(), "missing", "dir", "x.log")},
	} {
		var out bytes.Buffer
		a := newApp()
		a.cmd.SetOut(&out)
		a.cmd.SetErr(io.Discard)
		a.cmd.SetArgs(args)

		require.Error(t, a.execute(), name)
		assert.Empty(t, out.String(), "%s: nothing runs when logging cannot be configured", name)
		assert.Nil(t, a.closer, "%s: nothing is left open", name)
	}
}

func TestNewRootCmd_HasNoSecretFlags(t *testing.T) {
	cmd := newRootCmd()

	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		for _, secret := range []string{"token", "secret", "password", "key"} {
			assert.NotContains(t, f.Name, secret, "a credential never has a flag (ADR-078 S-1)")
		}
	})
}

// A failing command must still release the log file and give slog its previous
// default back, and the failure itself must reach the configured log.
func TestApp_Execute_CleansUpWhenTheCommandFails(t *testing.T) {
	silenceLogs(t)
	prev := slog.Default()
	path := filepath.Join(t.TempDir(), "trader.log")
	a := newApp()
	a.run = func(io.Writer) error { return errors.New("data is bad") }
	a.cmd.SetOut(io.Discard)
	a.cmd.SetArgs([]string{"--log-format", "json", "--log-output", path})

	err := a.execute()

	require.EqualError(t, err, "data is bad")
	assert.Nil(t, a.closer, "the log file is closed on failure")
	assert.Same(t, prev, slog.Default(), "slog's previous default is back, so later logs reach a usable logger")
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec))
	assert.Equal(t, "command failed", rec["msg"])
	assert.Equal(t, "ERROR", rec["level"])
	assert.Equal(t, "data is bad", rec["error"])
}

func TestApp_Execute_CanRunRepeatedlyWithoutLeakingAnOutput(t *testing.T) {
	silenceLogs(t)
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.log"), filepath.Join(dir, "second.log")

	for _, path := range []string{first, second} {
		a := newApp()
		a.run = func(io.Writer) error { slog.Info("ran"); return nil }
		a.cmd.SetOut(io.Discard)
		a.cmd.SetArgs([]string{"--log-output", path})
		require.NoError(t, a.execute())
	}

	// A log call after both runs does not reach either (now closed) file.
	slog.Info("after")
	for _, path := range []string{first, second} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(string(data), "msg=ran"), path)
		assert.NotContains(t, string(data), "msg=after", path)
	}
}

func TestApp_Cleanup_IsSafeToCallTwice(t *testing.T) {
	silenceLogs(t)
	a := newApp()
	a.cmd.SetOut(io.Discard)
	a.run = func(io.Writer) error { return nil }
	a.cmd.SetArgs([]string{"--log-output", filepath.Join(t.TempDir(), "x.log")})
	require.NoError(t, a.execute())

	require.NoError(t, a.cleanup())
	require.NoError(t, a.cleanup())
}

// slogDefault and restoreSlog let a test that runs the whole app put slog's
// default back, since the app installs its own for the run.
func slogDefault() *slog.Logger { return slog.Default() }

func restoreSlog(l *slog.Logger) { slog.SetDefault(l) }
