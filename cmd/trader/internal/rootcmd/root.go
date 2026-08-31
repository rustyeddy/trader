// Package rootcmd builds Trader's root Cobra command tree: the root
// command's own persistent logging flags and PersistentPreRunE
// (building the *slog.Logger every family's own service layer reads
// back via clictx.LoggerFromContext), plus attaching every command
// family (cmd/trader/data, cmd/trader/broker, ...).
//
// It lives under cmd/trader/internal so only cmd/trader and its own
// family packages can import it — nothing outside the CLI composition
// root has any business building or driving the full command tree.
// The context-logger helpers and TRADER_ environment-variable prefix
// themselves live one level lower, in cmd/trader/internal/clictx, not
// here: rootcmd imports every command family to attach its commands,
// so a family package cannot import rootcmd back without a cycle, but
// every family's own service.go still needs those two things — see
// clictx's own package doc comment.
//
// cmd/trader/main.go is the only production caller of New. Every
// family package's own integration-style tests (for example
// cmd/trader/data's command_test.go/mutation_test.go) import rootcmd
// too, as an external test package, to drive the real, fully-wired
// command tree exactly the way an operator invocation works — the
// same reason this package must be genuinely importable rather than
// folded into cmd/trader's own package main, which no other package
// could ever import (issue #201).
package rootcmd

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	cmdbacktest "github.com/rustyeddy/trader/cmd/trader/backtest"
	"github.com/rustyeddy/trader/cmd/trader/broker"
	"github.com/rustyeddy/trader/cmd/trader/data"
	"github.com/rustyeddy/trader/cmd/trader/execution"
	"github.com/rustyeddy/trader/cmd/trader/internal/clictx"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/logging"
)

// rootFlags holds the root command's persistent flag values. Cobra
// flag names (log-level, log-format, log-output) are chosen for CLI
// readability and are independent of logging.Config's own field names;
// buildLoggingConfig is the one place that maps between them.
type rootFlags struct {
	logLevel  string
	logFormat string
	logOutput string
}

// New builds the trader command tree: the root command, its
// persistent logging flags, and every command group. It performs no
// I/O and starts nothing on its own — a caller (main, or a test) drives
// execution via ExecuteContext.
//
// It also returns cleanup, which closes whatever logger output
// PersistentPreRunE actually built (a no-op if PersistentPreRunE never
// ran or never got that far — for example on a bare "trader --help").
// The caller must defer cleanup() around its own call to
// Execute(Context), unconditionally: see the "Logger lifetime" section
// below for why Cobra's own PersistentPostRunE hook cannot be trusted
// for this.
//
// # Context propagation
//
// PersistentPreRunE checks cmd.Context().Err() before doing anything
// else, including building the logger: a context already cancelled or
// deadline-exceeded when Execute(Context) is called — the normal result
// of main's SIGINT/SIGTERM handling firing before or during startup —
// fails the command immediately with that context's own error, rather
// than proceeding as if nothing happened. Every subcommand reaches its
// own RunE through the same cmd.Context(), so this is the one place
// cancellation needs to be checked explicitly at the framework level;
// individual commands additionally check ctx as their own work
// requires it once they exist (#109-#112).
//
// # Logger lifetime
//
// The logger's closer is deliberately not installed as
// PersistentPostRunE, as an earlier version of this function did.
// Cobra's Command.execute returns immediately when a command's own
// RunE returns a non-nil error — the PersistentPostRunE loop sits
// below that call, unreached on that path, not behind a defer. A
// failing leaf command (#109-#112) would therefore leak its log file
// descriptor on every single failure, exactly the case that matters
// most for an operator tailing a file-backed log. Returning the real
// closer here and requiring the caller to defer it around
// Execute(Context) instead guarantees cleanup runs on every return
// path, success or failure, the same way any other Go resource is
// deferred-closed — independent of which Cobra hook happened to fire.
func New() (*cobra.Command, func() error) {
	// Without this, Cobra runs only the *closest* PersistentPreRunE up
	// the command tree for a given invocation -- data's own
	// PersistentPreRunE (building the marketdata service) would
	// entirely shadow this one (building the logger), rather than
	// running after it, for every data subcommand. See
	// cmd/trader/data's own command.go doc comment.
	cobra.EnableTraverseRunHooks = true

	var flags rootFlags
	var closer io.Closer

	cmd := &cobra.Command{
		Use:   "trader",
		Short: "Trader is a framework for testing and executing algorithmic trading.",
		// SilenceUsage/SilenceErrors: trader prints its own error (see
		// main.run), so Cobra's default double-printing (usage banner
		// plus a second "Error:" line) is suppressed here.
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := cmd.Context().Err(); err != nil {
				return err
			}

			logCfg, err := buildLoggingConfig(cmd, flags)
			if err != nil {
				return err
			}
			logger, c, err := logging.New(logCfg)
			if err != nil {
				return err
			}
			closer = c
			cmd.SetContext(clictx.WithLogger(cmd.Context(), logger))
			return nil
		},
		// RunE, not a nil Run/RunE: Cobra treats a command with neither
		// as non-runnable and returns flag.ErrHelp *before*
		// PersistentPreRunE ever executes (Command.execute checks
		// Runnable() ahead of preRun). Without an explicit RunE here,
		// an invalid --log-format (or any other PersistentPreRunE
		// failure) would be silently ignored on a bare "trader"
		// invocation -- exactly the same gap data's New RunE
		// closes for "trader data" alone. The visible behavior is
		// unchanged (help text), but the flag validation this issue's
		// framework exists to prove now actually runs.
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().StringVar(&flags.logLevel, "log-level", "",
		"log level: DEBUG, INFO, WARN, or ERROR (default INFO)")
	cmd.PersistentFlags().StringVar(&flags.logFormat, "log-format", "",
		"log format: text or json (default text)")
	cmd.PersistentFlags().StringVar(&flags.logOutput, "log-output", "",
		"log output: stderr, stdout, or a file path (default stderr)")

	cmd.AddCommand(data.New())
	cmd.AddCommand(broker.New())
	cmd.AddCommand(execution.New())
	cmd.AddCommand(cmdbacktest.New())

	cleanup := func() error {
		if closer == nil {
			return nil
		}
		return closer.Close()
	}
	return cmd, cleanup
}

// buildLoggingConfig resolves a logging.Config from flags actually set
// on cmd (never an unset flag's empty zero value, which would
// incorrectly override logging.Config's own documented defaults),
// layered under the TRADER_LEVEL/TRADER_FORMAT/TRADER_OUTPUT
// environment variables, via the same config.Load every Trader
// composition root uses.
func buildLoggingConfig(cmd *cobra.Command, flags rootFlags) (logging.Config, error) {
	overrides := map[string]string{}
	if cmd.Flags().Changed("log-level") {
		overrides["level"] = flags.logLevel
	}
	if cmd.Flags().Changed("log-format") {
		overrides["format"] = flags.logFormat
	}
	if cmd.Flags().Changed("log-output") {
		overrides["output"] = flags.logOutput
	}

	return config.Load[logging.Config](config.Options{
		EnvPrefix: clictx.EnvPrefix,
		Environ:   os.Environ(),
		Overrides: overrides,
	})
}
