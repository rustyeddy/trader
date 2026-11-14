package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/internal/logging"
)

func main() {
	if newApp().execute() != nil {
		os.Exit(1)
	}
}

// logFlags are the logging options of every command. They carry no secret: a
// credential never has a flag (ADR-078 S-1).
type logFlags struct {
	level  string
	format string
	output string
}

func (f *logFlags) register(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVar(&f.level, "log-level", "INFO", "minimum log level: DEBUG, INFO, WARN or ERROR")
	pf.StringVar(&f.format, "log-format", "text", "log format: text or json")
	pf.StringVar(&f.output, "log-output", "stderr", "log destination: stderr, stdout or a file path to append to")
}

// app is the command and the logging resources it owns. Its execute is the one
// place that logs a failure and releases them, on every exit path.
type app struct {
	cmd   *cobra.Command
	flags logFlags
	run   func(io.Writer) error // the backtest; replaced in tests

	// getenv reads the process environment. cmd/trader is the only place that
	// does (ADR-078 S-1, ADR-089); tests replace it.
	getenv func(string) string

	prev   *slog.Logger // slog's default before install
	closer io.Closer    // the log output; nil when none is open
}

func newApp() *app {
	a := &app{run: trader.Run, getenv: os.Getenv}
	a.cmd = &cobra.Command{
		Use:           "trader",
		Short:         "Backtest the EMAX scenarios and print a summary of each",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return a.install()
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd.OutOrStdout())
		},
	}
	a.flags.register(a.cmd)
	a.cmd.AddCommand(newDataCmd(a))
	return a
}

// newRootCmd builds the trader command. With no subcommand it backtests every
// scenario and prints one summary each to stdout. Errors are returned to
// execute, which logs them, so cobra does not print them a second time.
func newRootCmd() *cobra.Command { return newApp().cmd }

// install builds the logger the flags describe and makes it slog's default,
// which the trading engine still logs through. This is the composition root:
// the one place the default is chosen (ADR-023). cleanup undoes it.
func (a *app) install() error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(a.flags.level)); err != nil {
		return fmt.Errorf("--log-level %q: %w", a.flags.level, err)
	}
	logger, closer, err := logging.New(logging.Config{Level: level, Format: a.flags.format, Output: a.flags.output})
	if err != nil {
		return err
	}
	a.prev, a.closer = slog.Default(), closer
	slog.SetDefault(logger)
	return nil
}

// cleanup restores slog's previous default and then releases the log output, so
// no later log call can target a closed file. It is safe to call more than once.
func (a *app) cleanup() error {
	if a.closer == nil {
		return nil
	}
	slog.SetDefault(a.prev)
	closer := a.closer
	a.closer, a.prev = nil, nil
	return closer.Close()
}

// execute runs the command. A failure is logged through the configured logger
// while it is still installed (or through the previous default when logging
// could not be configured), and the logging resources are released whether the
// command succeeded or failed.
func (a *app) execute() error {
	err := a.cmd.Execute()
	if err != nil {
		slog.Error("command failed", "error", err)
	}
	if cerr := a.cleanup(); cerr != nil && err == nil {
		err = cerr
	}
	return err
}
