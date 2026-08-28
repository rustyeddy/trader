// Package strategy defines Trader's broker-neutral strategy runtime
// contract (issue #210, M5-02): how a strategy receives market
// observations and emits order.Intent values, without knowing whether
// it is running inside a backtest or a future live session.
//
// # Scope
//
// Strategy is deliberately small — Describe, Start, and OnBar — per
// the M5-02 design review: TickHandler, FillHandler,
// AccountEventHandler, StateManager, and DataRequirement.NeedTicks are
// not published here. Each is additive, optional-capability surface
// area for a later issue with a concrete consumer to shape it against,
// not something to speculate into place now (the architecture
// document's own "small required core plus capability discovery"
// guidance).
//
// View is similarly minimal: Account() account.Snapshot is the one
// read it exposes today. A historical-bar lookup method is
// deliberately deferred until the replay/scheduler work (#212/#213)
// proves the access pattern a real backtest run needs — freezing it
// now risked designing against the wrong shape.
//
// # Dependency direction
//
// strategy depends only on order, marketdata, instrument, account,
// clock, id, num, and log/slog — never on broker, execution, risk, or
// pipeline (ADR-005, ADR-035). See boundary_test.go for the mechanical
// guard.
//
// # Intent construction and correlation
//
// OnBar returns fully-formed, canonical order.Intent values — never a
// parallel strategy.Intent DTO (ADR-005's own settled decision,
// reaffirmed on #210's own review). A strategy never touches Trader's
// ID-generation machinery directly to build one: Environment.Intents
// is a narrow IntentFactory capability the runtime injects, which
// generates deterministic IntentID/EventID/CorrelationID values and
// calls order.NewIntent on the strategy's behalf. The strategy still
// owns trading *semantics* — in particular, whether several intents
// returned from one OnBar call belong to one correlation group (for
// example a reversal expressed as an exit intent and an enter intent)
// — via IntentFactory.NewCorrelationID/WithCorrelation, without ever
// needing to know how a CorrelationID is actually generated. This
// keeps deterministic ID generation as runtime infrastructure that can
// later be represented cleanly across an out-of-process strategy
// protocol boundary, rather than something every strategy author
// reimplements.
package strategy
