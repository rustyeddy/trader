package main

import (
	"bytes"
	"encoding/json"
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

func TestNewRootCmd_LogsToTheConfiguredFileAsJSON(t *testing.T) {
	silenceLogs(t)
	t.Chdir("../..")
	path := filepath.Join(t.TempDir(), "trader.log")

	cmd := newRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"--log-level", "DEBUG", "--log-format", "json", "--log-output", path})
	require.NoError(t, cmd.Execute())

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

	cmd := newRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"--log-level", "ERROR", "--log-output", path})
	require.NoError(t, cmd.Execute())

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
		cmd := newRootCmd()
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(args)

		require.Error(t, cmd.Execute(), name)
		assert.Empty(t, out.String(), "%s: nothing runs when logging cannot be configured", name)
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
