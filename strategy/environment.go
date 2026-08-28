package strategy

import (
	"log/slog"

	"github.com/rustyeddy/trader/clock"
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
}
