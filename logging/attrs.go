package logging

import "log/slog"

// Canonical attribute key names, per issue #21. Using these names
// consistently is what makes log records correlatable across components,
// runs, and sessions — a component that invents its own key for the same
// concept (say, "order" instead of OrderID) breaks that correlation for
// anyone querying by the canonical name.
//
// CorrelationID and CausationID may hold plain strings for now, per #21,
// until Trader-owned ID types exist; nothing here assumes a particular ID
// representation.
const (
	// RunID identifies one backtest, scan, or live session run.
	RunID = "run_id"

	// SessionID identifies one live or paper trading session within a run.
	SessionID = "session_id"

	// AccountID identifies one broker account.
	AccountID = "account_id"

	// InstrumentID identifies one Trader instrument.
	InstrumentID = "instrument_id"

	// OrderID identifies one order.
	OrderID = "order_id"

	// CorrelationID groups every record produced while handling one
	// logical operation (a strategy decision, an order's full lifecycle),
	// even across component and process boundaries.
	CorrelationID = "correlation_id"

	// CausationID identifies the specific event or command that caused this
	// record's event, distinct from CorrelationID's broader grouping: many
	// records can share a CorrelationID while each has a different, more
	// specific CausationID.
	CausationID = "causation_id"

	// Component names the subsystem a child logger was scoped to — see
	// WithComponent.
	Component = "component"
)

// WithComponent returns a child logger scoped to component: every record it
// produces carries a Component attribute with that name, so records from
// different subsystems remain distinguishable after aggregation.
//
// This is a thin, named wrapper around slog.Logger.With for exactly one
// canonical attribute — not a general-purpose logger wrapper — so that
// "how do I get a component-scoped logger" has one obvious, documented
// answer instead of every call site choosing its own key name.
func WithComponent(l *slog.Logger, component string) *slog.Logger {
	return l.With(Component, component)
}
