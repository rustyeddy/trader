package strategy

import (
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
)

// History is an optional View capability exposing previously-closed
// bars for one of a strategy's own declared DataRequirements, strictly
// before the callback's own current timestamp (issue #214, M5-06
// review). It lives here rather than in a runtime-specific package
// (for example backtest) because historical lookback is strategy-
// facing, not specific to how a particular runtime replays or streams
// data: a future live session implements the identical capability
// from its own rolling market-data state, so a strategy written
// against History runs unchanged in either mode — the same "a strategy
// cannot tell which mode it is running in" guarantee the rest of this
// package already provides.
//
// A View implementing History must be immutable with respect to
// market-time visibility: retaining a View across OnBar calls and
// calling HistoryBars on it later must never expose an observation
// that only became visible after the View was constructed. A runtime
// enforces this by freezing each View's own visibility cutoff at
// construction time — see the concrete runtime's own doc comment for
// how (for example backtest.Scheduler's frozenView).
type History interface {
	// HistoryBars returns up to n of the most-recently-closed bars for
	// (instID, interval), strictly before the current callback's own
	// timestamp, oldest-first. ok is false if (instID, interval) was
	// never declared as one of this strategy's own
	// Descriptor.Requirements — History never makes arbitrary replayed
	// or streamed data queryable merely because the runtime happens to
	// have it. Fewer than n bars, with ok true, means fewer are
	// available yet (run start, or before this requirement's own data
	// begins); HistoryBars never pads and never returns an error for
	// this case.
	HistoryBars(instID instrument.ID, interval marketdata.Interval, n int) (bars []marketdata.Bar, ok bool)
}
