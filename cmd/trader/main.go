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
	if err := newRootCmd().Execute(); err != nil {
		slog.Error("backtest failed", "error", err)
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

// install builds the logger these flags describe and makes it slog's default,
// which the trading engine still logs through. This command is the composition
// root: the one place the default is chosen (ADR-023). The returned closer
// releases a log file and must be closed when the command ends.
func (f *logFlags) install() (io.Closer, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(f.level)); err != nil {
		return nil, fmt.Errorf("--log-level %q: %w", f.level, err)
	}
	logger, closer, err := logging.New(logging.Config{Level: level, Format: f.format, Output: f.output})
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	return closer, nil
}

// newRootCmd builds the trader command. With no subcommand it backtests every
// scenario and prints one summary each to stdout. Errors are returned to
// main, which logs them, so cobra does not print them a second time.
func newRootCmd() *cobra.Command {
	var flags logFlags
	var closer io.Closer
	cmd := &cobra.Command{
		Use:           "trader",
		Short:         "Backtest the EMAX scenarios and print a summary of each",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) (err error) {
			closer, err = flags.install()
			return err
		},
		PersistentPostRunE: func(*cobra.Command, []string) error {
			if closer == nil {
				return nil
			}
			c := closer
			closer = nil
			return c.Close()
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return trader.Run(cmd.OutOrStdout())
		},
	}
	flags.register(cmd)
	return cmd
}
