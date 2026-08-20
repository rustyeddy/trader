// Package logging establishes Trader's structured logging framework, as
// decided by issue #21 (M1-03), built directly on log/slog.
//
// # Ownership
//
// logging is a composition-root support package, the same role config plays
// for configuration: cmd/ binaries and test/example programs construct a
// *slog.Logger with New and inject it into the components that need it.
// Domain packages never construct their own logger, never read the process
// environment or flags to configure logging, and never depend on slog's
// mutable package-level default logger (slog.SetDefault, slog.Default, or
// the slog.Info/Warn/Error/Debug package-level functions, all of which read
// that default). They accept an injected *slog.Logger — or nothing at all,
// if silence is an acceptable default — through their constructors.
//
// This package deliberately does not define a logger wrapper type. Trader's
// architecture explicitly calls out "avoid a wrapper that merely duplicates
// every slog.Logger method": components accept and use *slog.Logger
// directly. What this package adds is everything around that: building one
// from typed configuration, canonical attribute names, context-propagated
// correlation, redaction, and test helpers.
//
// # Configuration
//
// Config is a plain tagged struct with no dependency on the config package;
// a composition root loads it however it likes, typically:
//
//	cfg, err := config.Load[logging.Config](opts)
//	logger, closer, err := logging.New(cfg)
//	defer closer.Close()
//
// This works with zero special-casing in config because slog.Level already
// implements encoding.TextUnmarshaler and encoding.TextMarshaler, parsing
// "DEBUG", "INFO", "WARN", and "ERROR" (case-insensitively) on its own.
//
// There is no Fatal level. slog has no native FATAL, and a logging package
// is the wrong place to hide process termination: an operator-actionable
// "the process must stop" decision is an Error-level log followed by an
// explicit os.Exit in the composition root, not a side effect buried inside
// a log call. See AGENTS.md for the reasoning.
//
// # Canonical attributes
//
// Correlating log records across a run, session, account, instrument,
// order, and cause requires agreement on attribute key names. The constants
// in attrs.go are that agreement: RunID, SessionID, AccountID,
// InstrumentID, OrderID, CorrelationID, and CausationID. Correlation and
// causation IDs may initially be plain strings, per #21, until Trader-owned
// ID types exist.
//
// # Components
//
// WithComponent scopes a logger to one architectural subsystem via the
// canonical Component attribute. attrs.go also defines a small, named
// vocabulary of component names (ComponentMarketData, ComponentBroker,
// ComponentAccount, ComponentOrders, ComponentPortfolio, ComponentStrategy,
// ComponentBacktest, ComponentExecution, ComponentService, ComponentCLI —
// per issue #126 and ADR-023), so "which subsystem is this" has one agreed
// spelling instead of every call site inventing its own. These are plain
// string constants, not a registry: there is no global lookup from a
// component name to a pre-built logger, and a composition root still
// constructs and scopes every logger explicitly.
//
// # Multiple outputs
//
// New builds exactly one sink. NewMulti (issue #127, ADR-023) builds
// several — the evaluated, accepted answer to ADR-023's own open question
// of whether Trader needs simultaneous outputs, for the primary use case
// it names: operator console output plus a persistent log file. Each sink
// keeps its own independent Level and Format, so a console sink can stay
// human-readable at INFO while a file sink captures DEBUG as JSON if a
// caller wants that; passing Configs that differ only in Output keeps
// every sink's behavior consistent by construction. The composition root
// still owns every sink's closer, exactly as with New — NewMulti's own
// returned io.Closer closes all of them and joins every close error
// rather than dropping any of them, and a sink that fails to build after
// an earlier one already opened never leaks that earlier sink's resource.
//
// Runtime reconfiguration (switching level, format, or destinations in a
// running process), syslog/journald output, and a production in-memory
// sink are deliberately not part of this package: ADR-023 records each as
// useful prior art from trader-first-try/log that is not required for
// Milestone 2.6, to be added only when a concrete requirement justifies
// it.
//
// # Redaction
//
// Wrap a sensitive value with Secret before logging it — logger.Info("auth",
// "password", logging.Secret(pw)) — rather than relying on a handler to
// guess which keys are sensitive from their names. New also installs a
// small built-in denylist of common sensitive key names as a second,
// best-effort layer; Secret is the mechanism that is actually reliable,
// because it is correct regardless of what the attribute is named.
//
// # Testing
//
// Use Discard for a component test that needs a valid logger but does not
// care about its output, and Capture for a test that needs to assert on
// what was logged: Capture returns a *Recorder whose Records method returns
// structured records directly, without parsing formatted console output.
package logging
