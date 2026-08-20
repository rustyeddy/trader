package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmd_HelpSucceeds(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"--help"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	require.NoError(t, err)
}

func TestNewRootCmd_DataGroupExists(t *testing.T) {
	root := newRootCmd()

	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "data" {
			found = true
		}
	}
	require.True(t, found, "trader data must exist as a command group")

	root.SetArgs([]string{"data", "--help"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))
	err := root.ExecuteContext(context.Background())
	require.NoError(t, err)
}

// TestNewRootCmd_PropagatesCancelledContext exercises the real "trader
// data" invocation, not a synthetic probe: data.go's own RunE (calling
// cmd.Help()) makes it Runnable, which is exactly what makes
// PersistentPreRunE's cancellation check reachable on the actual
// current command tree rather than only through a future leaf command
// (#109-#112) or a test-only stand-in.
func TestNewRootCmd_PropagatesCancelledContext(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"data"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := root.ExecuteContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewRootCmd_RejectsInvalidLogFormat(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"--log-format=bogus", "data"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
}

// newLoggerProbeCmd attaches a leaf command that captures the logger
// PersistentPreRunE built, via its own cmd.Context() -- something
// neither root nor data's RunE (which just calls cmd.Help()) exposes.
// *got is set when the probe runs, proving the logger travels all the
// way from root's PersistentPreRunE through to a real subcommand
// invocation, the same path a future leaf command (#109-#112) will
// use to build its own service calls.
func newLoggerProbeCmd(got **slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, args []string) error {
			*got = loggerFromContext(cmd.Context())
			return nil
		},
	}
}

// TestNewRootCmd_LoggerReachesSubcommandContext proves the framework's
// bootstrap wiring end to end, ahead of any real leaf command existing
// (#109-#112 add those): a probe command attached to the same tree
// newRootCmd built retrieves, through its own cmd.Context(), the exact
// logger PersistentPreRunE constructed from --log-level.
func TestNewRootCmd_LoggerReachesSubcommandContext(t *testing.T) {
	root := newRootCmd()
	var got *slog.Logger
	root.AddCommand(newLoggerProbeCmd(&got))
	root.SetArgs([]string{"--log-level=DEBUG", "probe"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, got.Handler().Enabled(context.Background(), slog.LevelDebug),
		"--log-level=DEBUG must actually configure the logger reachable from a subcommand")
}

func TestNewRootCmd_DefaultLoggingWhenNoFlagsOrEnv(t *testing.T) {
	root := newRootCmd()
	var got *slog.Logger
	root.AddCommand(newLoggerProbeCmd(&got))
	root.SetArgs([]string{"probe"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	require.NoError(t, err)
	require.False(t, got.Handler().Enabled(context.Background(), slog.LevelDebug),
		"the documented default level is INFO, not DEBUG")
	require.True(t, got.Handler().Enabled(context.Background(), slog.LevelInfo))
}

func TestNewRootCmd_EnvironmentVariableConfiguresLogger(t *testing.T) {
	t.Setenv("TRADER_LEVEL", "DEBUG")
	root := newRootCmd()
	var got *slog.Logger
	root.AddCommand(newLoggerProbeCmd(&got))
	root.SetArgs([]string{"probe"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	require.NoError(t, err)
	require.True(t, got.Handler().Enabled(context.Background(), slog.LevelDebug),
		"TRADER_LEVEL must configure the logger when no flag overrides it")
}

func TestNewRootCmd_FlagOverridesEnvironmentVariable(t *testing.T) {
	t.Setenv("TRADER_LEVEL", "DEBUG")
	root := newRootCmd()
	var got *slog.Logger
	root.AddCommand(newLoggerProbeCmd(&got))
	root.SetArgs([]string{"--log-level=WARN", "probe"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	require.NoError(t, err)
	require.False(t, got.Handler().Enabled(context.Background(), slog.LevelDebug),
		"an explicit flag must outrank the environment variable")
	require.True(t, got.Handler().Enabled(context.Background(), slog.LevelWarn))
}

func TestNewRootCmd_LogFormatFlagAppliesToLogger(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "trader.log")

	root := newRootCmd()
	// The message must be logged from inside RunE, before
	// PersistentPostRunE closes the file output -- logging after
	// ExecuteContext returns would write to an already-closed file.
	root.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, args []string) error {
			loggerFromContext(cmd.Context()).Info("test message")
			return nil
		},
	})
	root.SetArgs([]string{"--log-format=json", "--log-output=" + logPath, "probe"})
	root.SetOut(new(discard))
	root.SetErr(new(discard))

	err := root.ExecuteContext(context.Background())
	require.NoError(t, err)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(strings.TrimSpace(string(data)), "{"),
		"--log-format=json must produce JSON output, got: %s", data)
}

func TestLoggerFromContext_DefaultsWhenUnset(t *testing.T) {
	logger := loggerFromContext(context.Background())
	require.NotNil(t, logger)
}

// discard is a minimal io.Writer that keeps test output out of `go
// test -v`'s own output without importing io/ioutil or os for a
// throwaway /dev/null handle.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
