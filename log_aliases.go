package trader

// Transition shim re-exporting the log package (extracted from the root trader
// package) so existing callers compile unchanged during the migration. The
// module loggers are assigned once at log package init and reconfigured through
// a shared handler (Setup does not reassign them), so re-exporting them as vars
// is safe. Remove entries as callers move to github.com/rustyeddy/trader/log
// directly. See docs/pkg-migration.org.

import "github.com/rustyeddy/trader/log"

// Log types.
type (
	LogEntry  = log.LogEntry
	LogConfig = log.LogConfig
)

// Log functions.
var (
	Setup        = log.Setup
	Module       = log.Module
	Debug        = log.Debug
	Info         = log.Info
	Warn         = log.Warn
	Error        = log.Error
	Fatal        = log.Fatal
	Entries      = log.Entries
	ClearEntries = log.ClearEntries
)

// Module loggers.
var (
	L            = log.L
	Data         = log.Data
	BacktestLog  = log.BacktestLog
	IndicatorLog = log.IndicatorLog
	Replay       = log.Replay
	Strat        = log.Strat
)
