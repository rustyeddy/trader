package backtest

import (
	"errors"
	"fmt"
	"sort"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// ErrMixedAccountFills reports fills belonging to more than one
// account passed to DeriveTrades. Trade derivation is scoped to one
// account/broker at a time (matching backtest.Runner's own v0 scope,
// ADR-035) — mixing accounts would silently net together exposure
// that was never actually the same position.
var ErrMixedAccountFills = errors.New("backtest: fills belong to more than one account")

// ErrUntrackedPositionTransition reports an internal inconsistency
// between DeriveTrades' own position and open-trade bookkeeping: a
// fill produced a transition other than TransitionOpen while no trade
// was being tracked for that listing. This should never happen — it
// would indicate a bug in DeriveTrades itself, not a problem with the
// supplied fills — but is reported as a normal error rather than a
// panic, matching Trader's error-handling conventions.
var ErrUntrackedPositionTransition = errors.New("backtest: fill transition with no tracked open trade")

// TradeSet is what DeriveTrades produces from a fill stream: Closed
// holds every trade that fully closed, and Open holds any position
// still open once the fill stream ended. They are kept separate
// deliberately — the issue's own "completed trades" goal means a
// caller iterating Closed never has to remember that some entries are
// not actually finished round trips. A caller that also needs
// in-progress exposure (for example, a live equity curve) consults
// Open explicitly instead.
type TradeSet struct {
	Closed []order.Trade
	Open   []order.Trade
}

// tradeKey groups fills by the (account, listing) pair whose Position
// they affect, mirroring the same account-scoped grouping every broker
// adapter's own position bookkeeping already uses (see
// adapters/broker/sim's own positionKey).
type tradeKey struct {
	instrumentID instrument.ID
	provider     string
	venue        string
}

func keyForListing(l instrument.Listing) tradeKey {
	return tradeKey{instrumentID: l.InstrumentID(), provider: l.Provider(), venue: l.Venue()}
}

// runningPosition is DeriveTrades' own per-listing bookkeeping: the
// current Position (the zero Position, with has false, before the
// first fill), kept in lock-step with whichever order.Trade is
// currently open for the same listing.
type runningPosition struct {
	position order.Position
	has      bool
}

// DeriveTrades groups fills, in delivery order (broker.Event.Sequence
// order — see Runner's own doc comment for how it obtains that order),
// into completed and still-open order.Trade values (issue #217,
// M5-09). It replays each fill's effect on a running per-listing
// Position via order.ApplyFillToPosition — the same broker-neutral
// position/PnL math the sim broker adapter itself uses to update its
// own authoritative account state — so for any Trader component built
// on that shared function, a derived trade's RealizedPnL cannot
// diverge from it by construction; DeriveTrades computes no financial
// arithmetic of its own beyond grouping and summing values
// order.ApplyFillToPosition already produced. This is not itself a
// claim about an external broker's own accounting rules, which may
// differ — reconciling derived trades against that broker's own
// account snapshots/events remains the actual proof of authority in
// that case, exactly as it would for any other derived view.
//
// DeriveTrades is a pure function over fills: it performs no I/O and
// consults no broker/account state beyond what the fills themselves
// carry. It is the caller's responsibility (Runner, for a live
// backtest run) to obtain fills from the account's own authoritative
// event stream in delivery order and pass them here — see Runner's own
// doc comment for why obtaining events and deriving trades from them
// are kept as separate concerns.
//
// Every fill must belong to the same account; a fill naming a
// different account.AccountID than the first fill seen is reported as
// ErrMixedAccountFills.
//
// # Trade grouping
//
// A fill that opens a new position from flat starts a new Trade. A
// same-side fill against an existing position (TransitionIncrease)
// adds to that Trade's EntryFillIDs. An opposite-side fill that
// reduces or exactly closes an existing position appends to
// ExitFillIDs and accumulates RealizedPnL; an exact close also sets
// ClosedAt and moves the Trade into TradeSet.Closed.
//
// A reversal — one fill whose quantity exceeds the existing position,
// so it closes the old position and opens a new one in the opposite
// direction — closes the old Trade (the fill's ID is appended to its
// ExitFillIDs, exactly as a normal close) and, using the very same
// fill, opens a brand-new Trade in the new direction (the same fill's
// ID becomes that new Trade's sole EntryFillIDs[0]). This one FillID
// deliberately appears in both trades: it is one broker execution that
// did both things economically, not a double-counted event.
//
// # Cost attribution
//
// A fill's own order.Fill.Commission (nil is treated as no cost) is
// attributed entirely to whichever single Trade the fill's whole
// quantity applies to — except a reversal fill, whose commission is
// split pro-rata by quantity between the trade it closes and the trade
// it opens: closingShare = commission * (closedQty / fillQty), and
// openingShare = commission - closingShare, the exact remainder, so
// the two shares always sum back to the original commission regardless
// of rounding.
//
// # Still-open trades
//
// Any listing with a non-flat running position when the fill stream
// ends produces a Trade in TradeSet.Open, with ClosedAt left zero and
// RealizedPnL/Costs reflecting whatever partial exits it has already
// seen — legitimate per order.NewTrade's own validation. TradeSet.Open
// is sorted by OpenedAt for determinism, since map iteration order is
// not otherwise stable.
func DeriveTrades(fills []order.Fill) (TradeSet, error) {
	var (
		accountID   id.AccountID
		haveAccount bool
		positions   = map[tradeKey]runningPosition{}
		openTrades  = map[tradeKey]*order.Trade{}
		closed      []order.Trade
	)

	for _, fill := range fills {
		if !haveAccount {
			accountID = fill.AccountID
			haveAccount = true
		} else if fill.AccountID != accountID {
			return TradeSet{}, fmt.Errorf("%w: fill %s belongs to %s, expected %s", ErrMixedAccountFills, fill.FillID, fill.AccountID, accountID)
		}

		key := keyForListing(fill.Listing)
		currency := fill.Listing.Spec().SettlementCurrency()
		rp := positions[key]
		existingQty := rp.position.Quantity

		transition, err := order.ApplyFillToPosition(rp.position, rp.has, fill.AccountID, fill.Listing, currency, fill.Side, fill.Price, fill.Quantity)
		if err != nil {
			return TradeSet{}, fmt.Errorf("backtest: deriving trades: applying fill %s: %w", fill.FillID, err)
		}
		positions[key] = runningPosition{position: transition.Position, has: transition.Position.Side != order.Flat}

		zero, err := num.ParseMoney("0", currency)
		if err != nil {
			return TradeSet{}, fmt.Errorf("backtest: deriving trades: %w", err)
		}
		cost := zero
		if fill.Commission != nil {
			cost = *fill.Commission
		}

		switch transition.Transition {
		case order.TransitionOpen:
			openTrades[key] = &order.Trade{
				AccountID:    fill.AccountID,
				Listing:      fill.Listing,
				Side:         transition.Position.Side,
				EntryFillIDs: []id.FillID{fill.FillID},
				OpenedAt:     fill.Timestamp,
				RealizedPnL:  transition.RealizedPnL,
				Costs:        cost,
			}

		case order.TransitionIncrease:
			t, ok := openTrades[key]
			if !ok {
				return TradeSet{}, fmt.Errorf("%w: fill %s (increase)", ErrUntrackedPositionTransition, fill.FillID)
			}
			t.EntryFillIDs = append(t.EntryFillIDs, fill.FillID)
			if t.Costs, err = t.Costs.Add(cost); err != nil {
				return TradeSet{}, fmt.Errorf("backtest: deriving trades: accumulating costs for fill %s: %w", fill.FillID, err)
			}

		case order.TransitionReduce, order.TransitionClose:
			t, ok := openTrades[key]
			if !ok {
				return TradeSet{}, fmt.Errorf("%w: fill %s (%s)", ErrUntrackedPositionTransition, fill.FillID, transition.Transition)
			}
			t.ExitFillIDs = append(t.ExitFillIDs, fill.FillID)
			if t.RealizedPnL, err = t.RealizedPnL.Add(transition.RealizedPnL); err != nil {
				return TradeSet{}, fmt.Errorf("backtest: deriving trades: accumulating realized pnl for fill %s: %w", fill.FillID, err)
			}
			if t.Costs, err = t.Costs.Add(cost); err != nil {
				return TradeSet{}, fmt.Errorf("backtest: deriving trades: accumulating costs for fill %s: %w", fill.FillID, err)
			}
			if transition.Transition == order.TransitionClose {
				t.ClosedAt = fill.Timestamp
				finalized, err := order.NewTrade(*t)
				if err != nil {
					return TradeSet{}, fmt.Errorf("backtest: deriving trades: finalizing closed trade for fill %s: %w", fill.FillID, err)
				}
				closed = append(closed, finalized)
				delete(openTrades, key)
			}

		case order.TransitionReverse:
			t, ok := openTrades[key]
			if !ok {
				return TradeSet{}, fmt.Errorf("%w: fill %s (reverse)", ErrUntrackedPositionTransition, fill.FillID)
			}

			closingCost, openingCost, err := splitCommission(existingQty, fill.Quantity, cost)
			if err != nil {
				return TradeSet{}, fmt.Errorf("backtest: deriving trades: splitting commission for reversal fill %s: %w", fill.FillID, err)
			}

			t.ExitFillIDs = append(t.ExitFillIDs, fill.FillID)
			if t.RealizedPnL, err = t.RealizedPnL.Add(transition.RealizedPnL); err != nil {
				return TradeSet{}, fmt.Errorf("backtest: deriving trades: accumulating realized pnl for fill %s: %w", fill.FillID, err)
			}
			if t.Costs, err = t.Costs.Add(closingCost); err != nil {
				return TradeSet{}, fmt.Errorf("backtest: deriving trades: accumulating costs for fill %s: %w", fill.FillID, err)
			}
			t.ClosedAt = fill.Timestamp
			finalized, err := order.NewTrade(*t)
			if err != nil {
				return TradeSet{}, fmt.Errorf("backtest: deriving trades: finalizing reversed trade for fill %s: %w", fill.FillID, err)
			}
			closed = append(closed, finalized)

			openTrades[key] = &order.Trade{
				AccountID:    fill.AccountID,
				Listing:      fill.Listing,
				Side:         transition.Position.Side,
				EntryFillIDs: []id.FillID{fill.FillID},
				OpenedAt:     fill.Timestamp,
				RealizedPnL:  zero,
				Costs:        openingCost,
			}
		}
	}

	open := make([]order.Trade, 0, len(openTrades))
	for _, t := range openTrades {
		finalized, err := order.NewTrade(*t)
		if err != nil {
			return TradeSet{}, fmt.Errorf("backtest: deriving trades: finalizing open trade: %w", err)
		}
		open = append(open, finalized)
	}
	sort.Slice(open, func(i, j int) bool { return lessOpenTrade(open[i], open[j]) })

	return TradeSet{Closed: closed, Open: open}, nil
}

// lessOpenTrade gives TradeSet.Open a total, deterministic order. Map
// iteration order (openTrades, above) is not otherwise stable, and
// OpenedAt alone is not a total order: multiple instruments commonly
// open on the same bar and therefore share an identical timestamp.
// Listing identity breaks that tie; EntryFillIDs[0] — guaranteed
// non-empty and unique per order.NewTrade's own validation — is the
// final, absolute tiebreak for the case two trades share both a
// timestamp and a listing, which cannot happen for two *simultaneously
// open* trades on one listing (only one can be open at a time) but is
// kept for defensiveness rather than assumed impossible by this
// function itself.
func lessOpenTrade(a, b order.Trade) bool {
	if !a.OpenedAt.Equal(b.OpenedAt) {
		return a.OpenedAt.Before(b.OpenedAt)
	}
	al, bl := a.Listing, b.Listing
	if al.InstrumentID() != bl.InstrumentID() {
		return al.InstrumentID().String() < bl.InstrumentID().String()
	}
	if al.Provider() != bl.Provider() {
		return al.Provider() < bl.Provider()
	}
	if al.Venue() != bl.Venue() {
		return al.Venue() < bl.Venue()
	}
	return a.EntryFillIDs[0].String() < b.EntryFillIDs[0].String()
}

// splitCommission divides commission pro-rata by quantity between the
// portion of a reversal fill that closed the existing position
// (existingQty) and the portion that opened the new one
// (fillQty-existingQty). openingShare is computed as the exact
// remainder (commission minus closingShare) rather than by its own
// division, so the two shares always sum back to commission exactly
// regardless of rounding.
func splitCommission(existingQty, fillQty num.Quantity, commission num.Money) (closingShare, openingShare num.Money, err error) {
	ratio, err := existingQty.Div(fillQty)
	if err != nil {
		return num.Money{}, num.Money{}, err
	}
	closingShare, err = commission.MulRate(ratio)
	if err != nil {
		return num.Money{}, num.Money{}, err
	}
	openingShare, err = commission.Sub(closingShare)
	if err != nil {
		return num.Money{}, num.Money{}, err
	}
	return closingShare, openingShare, nil
}
