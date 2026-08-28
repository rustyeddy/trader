package strategy

import (
	"context"

	"github.com/rustyeddy/trader/order"
)

// Strategy is Trader's broker-neutral strategy runtime contract
// (ADR-005): a policy that interprets market/portfolio state and
// emits order.Intent values, never a broker handle and never an order
// submission call. The same Strategy value runs identically inside a
// backtest or a future live session — a strategy cannot tell which
// mode it is running in, since Environment and View expose only
// capabilities and read-only state, never a mode flag.
//
// The contract is deliberately small; see the package doc comment for
// why optional capability interfaces (tick handling, fill handling,
// account-event handling, state persistence) are not published here
// yet.
type Strategy interface {
	// Describe returns this strategy's identity and data requirements.
	// A runner calls this once, before Start, to validate its own
	// environment (data availability, warm-up) ahead of the first
	// OnBar call.
	Describe() Descriptor

	// Start is called once, before any OnBar call, with this run's own
	// Environment. A strategy performs one-time setup here — it does
	// not yet have a View, since no bar has been replayed.
	Start(ctx context.Context, env Environment) error

	// OnBar is called once per completed bar this strategy required
	// (Descriptor.Requirements), after any configured warm-up period
	// has elapsed. It returns zero or more order.Intent values built
	// via Environment.Intents — never a broker call, never a direct
	// order submission.
	OnBar(ctx context.Context, event BarEvent, view View) ([]order.Intent, error)
}
