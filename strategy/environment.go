package strategy

import (
	"log/slog"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/journal"
)

// Environment is Start's own injected-dependency bundle: capabilities
// only, never configuration (issue #210's own review) — a strategy's
// own parameters remain strongly-typed configuration supplied when the
// concrete Strategy value is constructed, not a field here.
//
// Environment carries no hidden global: a strategy that only ever
// calls time through Clock, and only ever builds an Intent through
// Intents, is reproducible across independent runs sharing the same
// initial Clock/Intents state — the same determinism guarantee every
// other M3/M4 component already provides via injected dependencies.
type Environment struct {
	// Clock is the sole time source a strategy may consult.
	Clock clock.Clock
	// Intents builds every order.Intent this strategy emits. See
	// IntentFactory's own doc comment for why a strategy never touches
	// Trader's ID-generation machinery directly.
	Intents IntentFactory
	// Logger receives this strategy's own structured records, if any.
	// A nil Logger is never handed to a strategy; a runner injects
	// logging.Discard() when the caller supplied none, matching every
	// other Trader composition-root convention.
	Logger *slog.Logger

	// RunID identifies the current run, for a strategy that journals
	// its own records via Journal (below) — every journal.Record it
	// builds must carry this same RunID.
	RunID id.RunID
	// Journal is the optional capability letting a strategy record its
	// own research-side decision evidence (ADR-044, issue #253, EMA-08)
	// via journal.KindSignal records — ordered and durable exactly like
	// every other journal record, using the run's own real Recorder, so
	// a strategy's evidence interleaves correctly with the
	// intent/proposal/decision/order/fill records a runner journals
	// around it. Unlike Clock/Intents, Journal may be nil — a directly
	// constructed Environment (a unit test, for example) that never
	// sets it is not thereby invalid, and a strategy that chooses not
	// to record decision evidence never needs to set it up. A strategy
	// that does use it must nil-check before calling Record.
	Journal journal.Recorder
}
