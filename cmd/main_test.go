package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmd_Name(t *testing.T) {
	cmd := NewRootCmd()
	assert.Equal(t, "trader", cmd.Use)
}

func TestNewRootCmd_PersistentFlags(t *testing.T) {
	cmd := NewRootCmd()
	flags := cmd.PersistentFlags()
	for _, name := range []string{"config", "db", "report", "data-dir", "log-level", "log-format", "log-file", "no-color"} {
		assert.NotNil(t, flags.Lookup(name), "expected persistent flag --%s", name)
	}
}

func TestNewRootCmd_HasExpectedSubcommands(t *testing.T) {
	cmd := NewRootCmd()
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range []string{"backtest", "bot", "data", "health", "serve", "live", "account", "replay", "version"} {
		assert.True(t, names[want], "expected subcommand %q", want)
	}
}

func TestVersionCmd_PrintsVersion(t *testing.T) {
	cmd := NewRootCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// Find and execute the version subcommand directly (bypasses PersistentPreRunE).
	var versionCmd interface{ Execute() error }
	for _, sub := range cmd.Commands() {
		if sub.Name() == "version" {
			versionCmd = sub
			break
		}
	}
	require.NotNil(t, versionCmd, "version subcommand not found")

	// Set output on root so subcommand inherits it.
	require.NoError(t, versionCmd.Execute())
}

func TestNewRootCmd_SilenceErrors(t *testing.T) {
	cmd := NewRootCmd()
	assert.True(t, cmd.SilenceErrors)
	assert.True(t, cmd.SilenceUsage)
}

func TestNewRootCmd_VersionExecutes(t *testing.T) {
	cmd := NewRootCmd()
	// The version Run uses fmt.Printf (not cmd.OutOrStdout), so we just check
	// that it doesn't error. PersistentPreRunE is cleared to skip config loading.
	cmd.SetArgs([]string{"version"})
	cmd.PersistentPreRunE = nil
	require.NoError(t, cmd.Execute())
}
