package backtest

import (
	"context"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// IntrabarAdvancer is a required backtest-runtime capability that
// advances the broker's own per-bar resting-order-trigger machinery
// (ADR-026) once per bar (issue #338), closing the gap
// MarketObserver's own doc comment already documented: "[ADR-026 is]
// a broker-side Advance-shaped operation... still-deferred." Without
// this, a resting Limit/Stop order (for example the protective stop
// order.IntentAdjustStop now places and ratchets, issues #336/#337)
// appears to work at submission and then silently never fills.
//
// AdvanceBar takes a full bar's OHLC, not just its Close — unlike
// MarketObserver.ObserveMark, which this capability is added
// *alongside*, not in place of (see AdvanceBar's own call site in
// Scheduler for why removing MarketObserver is a deliberately
// out-of-scope cleanup rather than done here). The underlying
// operation (adapters/broker/sim's accountState.advance) already
// revalues marks from Close as its own first step, so calling both
// per bar is redundant but harmless — not a correctness bug, just an
// accepted, documented inefficiency until a future issue removes
// MarketObserver once every composition root has migrated.
//
// The signature deliberately uses only already-shared domain types
// (instrument.Listing, num.Price, time.Time), matching
// MarketObserver's own convention: an adapter's own account handle
// (see adapters/broker/sim) can satisfy this structurally without
// importing backtest. listing, not merely instrument.ID, is required
// because the underlying broker-side matching is by the exact Listing
// a resting order was submitted against (accountState.advance's own
// "o.Request.Listing != obs.Listing" check, ADR-026) — not by
// instrument identity alone.
type IntrabarAdvancer interface {
	AdvanceBar(ctx context.Context, listing instrument.Listing, open, high, low, close num.Price, at time.Time) error
}
