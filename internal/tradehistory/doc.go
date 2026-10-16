// Package tradehistory reconstructs completed trades from account activity
// (issue #512, ADR-082): a broker-neutral, derived view over the activity
// ledger. It reads accountactivity.Activity values and returns trades; it
// stores nothing, performs no I/O and reads no clock, and the same activities
// in any order produce byte-identical output.
//
// # A trade is a position cycle
//
// A completed trade runs flat, to a position, to flat again, for one account,
// instrument and side. Entries and exits may each be many fills: scale-in and
// scale-out stay one trade. A fill that closes a position and opens the
// opposite one belongs to two trades. Provider trade IDs do not define the
// boundary; where the broker supplies per-trade effects they become the legs
// of the trade (with their exact prices and units) and a cross-check, so
// OANDA's richer linkage is kept without making the model OANDA-shaped.
//
// # Amounts, and the spread-cost rule
//
// Every amount is signed account-centrically. GrossPL is the sum of
// broker-reported realized P/L; it is never recomputed from prices. SpreadCost
// is an economic cost attribution already embedded in that realized P/L (a buy
// executes at the ask, a sell at the bid), so it is aggregated and shown but
// NOT added into Net again:
//
//	Net = GrossPL + Commission + Financing + OtherFees
//
// A spread-neutral, mid-price P/L would be a different starting point and
// belongs to execution-quality analysis (#513), where spread cost can become
// an explicit term.
//
// # Unknown is not zero
//
// A component total is known only when every fill that contributes to it
// supplied it; a reported zero is known. Contribution is role-aware: realized
// P/L and financing are realized by the closing, so a pure entry that omits
// them does not make the trade's totals unknown, while a closing fill that
// omits them does. A partial sum is never presented as a
// complete one. An unknown SpreadCost does not make Net unknown, because it is
// not part of Net; an unknown Commission, Financing or OtherFees does.
//
// All totals are Derived values naming exactly what was summed. When a fill
// spans two trades and a cost cannot be separated from provider facts, it is
// apportioned by units and the method says so; if there is no defensible basis
// the component stays unknown. A proportional share is never presented as
// broker-reported.
//
// # Arithmetic
//
// Quantities and prices are exact. A weighted average price is the sum of each
// leg's notional (price times quantity, rounded half to even to 8 decimals)
// divided by the total quantity, rounded half to even, and is marked Derived
// with that method. It needs the instrument's quote currency, so it is
// currently available for currency pairs only and unknown otherwise.
//
// # What is outside a completed trade
//
// Positions still open are returned separately as OpenPositions, never dropped
// or force-closed. Standalone financing and fee activities are listed as
// Unattributed: the ledger does not yet carry OANDA's per-position daily
// financing breakdown (issue #522). An exit with no known entry, such as when
// the data window starts mid-trade, is listed as an Orphan and its costs are
// not attributed.
package tradehistory
