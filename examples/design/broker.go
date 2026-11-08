package main

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"
)

type broker struct {
	id       string
	accounts map[string]*account
	orders   map[string][]*order // submitted orders awaiting execution, by instrument, for every account
	seq      int                 // last order id issued

	// margin and closeout decide when and what to liquidate. Nil means
	// defaultMarginPolicy and closeWorstLoser.
	margin   marginPolicy
	closeout closeoutPolicy

	// completeCloseout makes a liquidation close every position of the account.
	// By default it is progressive: one position at a time, as the closeout
	// policy chooses, until the margin policy no longer demands more.
	completeCloseout bool

	// completed keeps every order that has reached a terminal state (filled,
	// rejected by the broker or cancelled), in the order it happened, for the
	// journal. Only complete appends to it. The broker's own liquidations are in
	// it too; closeouts picks them out by their reason.
	completed []*order
}

func newBroker(id string) *broker {
	return &broker{
		id:       id,
		accounts: make(map[string]*account),
		orders:   make(map[string][]*order),
	}
}

func (br *broker) addAccount(id string, balance money) *account {
	act := newAccount(id, balance)
	br.accounts[id] = act
	return act
}

// submit has the order's account size it, assigns it an id and queues it to
// await execution. The order is filled (or rejected) on a later bar by onBar;
// submit itself never fills. An expected rejection is recorded on the order
// and returned for the caller to classify.
func (br *broker) submit(ctx context.Context, o *order) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	act, ok := br.accounts[o.accountID]
	if !ok {
		return fmt.Errorf("submit to broker %s: %w: no account %q", br.id, errUnknownAccount, o.accountID)
	}

	if o.action == open {
		// A margin closeout is not undone by a new open on the same observation;
		// later re-entry rules are a future policy.
		if !o.Time.After(act.closedOutAt) {
			return reject(o, o.Time, "%w: account %s at %s",
				errCloseoutGuard, o.accountID, act.closedOutAt.Format(time.DateOnly))
		}
		if br.hasPendingOpen(o.accountID, o.instrument) {
			return reject(o, o.Time, "%w: account %s on %s", errOpenPending, o.accountID, o.instrument)
		}
	}

	if err := act.size(o, br.reservedMargin(o.accountID)); err != nil {
		return err
	}

	if o.id == "" {
		o.id = br.nextID()
	}
	slog.InfoContext(ctx, "order submitted",
		"broker", br.id,
		"account", o.accountID,
		"order", o.id,
		"instrument", o.instrument,
		"action", o.action,
		"direction", o.direction,
		"type", o.orderType,
		"units", o.units)
	br.enqueue(o)
	return nil
}

// enqueue marks the order submitted and adds it to the instrument's queue.
func (br *broker) enqueue(o *order) {
	o.record(stateSubmitted, o.Time, "")
	br.orders[o.instrument] = append(br.orders[o.instrument], o)
}

// cancel withdraws the account's waiting order id at time at, on request. The
// order ends cancelled, leaves the queue and releases the margin it reserved.
// An order that is not waiting (it filled, expired or was cancelled already, or
// belongs to another account) is an expected rejection, errNoOrder.
func (br *broker) cancel(ctx context.Context, accountID, orderID string, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n := br.cancelWhere(at, "cancelled by request", func(o *order) bool {
		return o.id == orderID && o.accountID == accountID
	})
	if n == 0 {
		return rejected("cancel %s on broker %s: %w", orderID, br.id, errNoOrder)
	}
	return nil
}

// cancelWhere cancels, at time at, every waiting order that match accepts and
// reports how many. Instruments are visited in a fixed order so the journal is
// replayable.
func (br *broker) cancelWhere(at time.Time, reason string, match func(*order) bool) int {
	n := 0
	for _, instrument := range slices.Sorted(maps.Keys(br.orders)) {
		for _, o := range slices.Clone(br.orders[instrument]) {
			if match(o) {
				o.record(stateCancelled, at, reason)
				br.complete(o)
				n++
			}
		}
	}
	return n
}

// hasPendingOpen reports whether the account has a submitted open waiting on the
// instrument.
func (br *broker) hasPendingOpen(accountID, instrument string) bool {
	return slices.ContainsFunc(br.orders[instrument], func(o *order) bool {
		return o.accountID == accountID && o.action == open
	})
}

// nextID issues the next order id.
func (br *broker) nextID() string {
	br.seq++
	return fmt.Sprintf("%s-%d", br.id, br.seq)
}

// reservedMargin is the margin held by the account's submitted opens that have
// not filled yet, at each order's reference price. Closes reserve nothing.
func (br *broker) reservedMargin(accountID string) money {
	var total money
	for _, orders := range br.orders {
		for _, o := range orders {
			if o.accountID == accountID && o.action == open {
				total += marginFor(o.instrument, o.units, o.price)
			}
		}
	}
	return total
}

// phases is how many passes onBar makes over an instrument's queue; see phase.
const phases = 4

// phase is the pass of a bar in which the order is considered: market orders
// first (they fill at the open), then resting entries, then protective stops,
// then take profits. The order between entries is unobservable (one pending
// open per account and instrument); stops before targets is the pessimistic
// choice when a bar reaches both.
func (o *order) phase() int {
	switch {
	case o.orderType == marketOrder:
		return 0
	case o.action == open:
		return 1
	case o.orderType == stopOrder:
		return 2
	}
	return 3
}

// onBar executes the orders waiting on b's instrument for every account, then
// values each account's positions at the bar's close and applies the margin
// rules. The trader calls it at the start of every bar, before the strategy
// sees the bar, so an order submitted on bar N can first fill on bar N+1 and
// never on the bar that produced it.
//
// The open is the one price a bar is known to have printed first, so the
// orders it triggers go first, whatever else the bar later does. Only then do
// the orders that depend on the rest of the range get their turn, in phases
// (see phase); an exit placed by an entry filled earlier is live in the same
// stage.
//
// Assumption: a bar gives only open, high, low and close, not the path between
// the extremes. After the open we take entries before stops and stops before
// targets, each against the bar's whole range. That is pessimistic for a stop
// on the bar a limit or stop entry fills (the low may have come before the
// entry) and optimistic for a target on that bar (the high may have). See
// todo.org for the options.
func (br *broker) onBar(ctx context.Context, b bar) error {
	br.expire(b)
	atOpen := b
	atOpen.h, atOpen.l = b.o, b.o // all that is known to have happened so far
	for _, seen := range []bar{atOpen, b} {
		for phase := 0; phase < phases; phase++ {
			for _, o := range slices.Clone(br.orders[b.instrument]) {
				if o.phase() != phase {
					continue
				}
				if err := br.processOrder(ctx, seen, o); err != nil {
					return err
				}
			}
		}
	}

	br.trailStops(b)
	for _, act := range br.accounts {
		act.mark(b)
	}
	for _, id := range slices.Sorted(maps.Keys(br.accounts)) {
		if err := br.enforceMargin(ctx, b, br.accounts[id]); err != nil {
			return fmt.Errorf("margin check %s: %w", id, err)
		}
	}
	return nil
}

// expire cancels the orders waiting on b's instrument whose expiry has been
// reached, before the bar can execute them.
func (br *broker) expire(b bar) {
	br.cancelWhere(b.Time, "expired", func(o *order) bool {
		return o.instrument == b.instrument && !o.expires.IsZero() && !b.Before(o.expires)
	})
}

// enforceMargin applies the margin rules to one account on bar b. A warning is
// logged. Liquidation cancels the account's unfilled opens, then closes
// positions one at a time, as the closeout policy chooses. A progressive
// closeout stops as soon as the margin policy is satisfied; a complete one
// closes everything.
func (br *broker) enforceMargin(ctx context.Context, b bar, act *account) error {
	policy, pick := br.margin, br.closeout
	if policy == nil {
		policy = defaultMarginPolicy
	}
	if pick == nil {
		pick = closeWorstLoser
	}

	status, err := policy(act)
	if err != nil {
		return err
	}
	if status == marginWarning {
		slog.Warn("margin warning", "broker", br.id, "account", act.id, "time", b.Time)
	}
	if status != marginLiquidate {
		return nil
	}

	slog.Warn("margin closeout", "broker", br.id, "account", act.id, "time", b.Time)
	act.closedOutAt = b.Time
	br.cancelWhere(b.Time, "margin closeout", func(o *order) bool {
		return o.accountID == act.id && o.action == open
	})
	for status == marginLiquidate || br.completeCloseout {
		pos, err := pick(act)
		if err != nil {
			return err
		}
		if pos == nil {
			return nil // nothing left to close
		}
		if err := br.closePosition(b, act, pos, reasonMarginCloseout, pos.markPrice); err != nil {
			return err
		}
		if status, err = policy(act); err != nil {
			return err
		}
	}
	return nil
}

// placeExits queues the protective stop and the take profit, when the filled
// open o carries them, as resting closes the broker owns.
func (br *broker) placeExits(o *order) {
	if o.stopLoss > 0 {
		br.placeExit(o, stopOrder, reasonStopLoss, o.stopLoss).trail = o.trail
	}
	if o.takeProfit > 0 {
		br.placeExit(o, limitOrder, reasonTakeProfit, o.takeProfit)
	}
}

func (br *broker) placeExit(o *order, typ orderType, why orderReason, p price) *order {
	x := &order{
		id:         br.nextID(),
		accountID:  o.accountID,
		instrument: o.instrument,
		direction:  o.direction, // the direction of the position it closes
		action:     close,
		orderType:  typ,
		reason:     why,
		price:      p,
		Time:       o.Time,
	}
	x.record(stateRequested, x.Time, "")
	br.enqueue(x)
	return x
}

// trailStops moves the trailing stops waiting on b's instrument toward profit:
// a long's stop up to the bar's high less the trail, a short's down to the low
// plus it, never back. It runs after the bar's executions, so a stop is always
// checked at its level from before the bar and a bar can never both raise a
// stop and hit the raised level: the path within the bar is unknown, and this
// takes no view on it. Each move is an event in the order's history.
func (br *broker) trailStops(b bar) {
	for _, o := range br.orders[b.instrument] {
		if o.reason != reasonStopLoss || o.trail <= 0 {
			continue
		}
		var moved price
		if o.direction == long {
			moved = max(o.price, b.h-o.trail)
		} else {
			moved = min(o.price, b.l+o.trail)
		}
		if moved != o.price {
			o.price = moved
			o.record(stateSubmitted, b.Time, fmt.Sprintf("trailed to %d", moved))
		}
	}
}

// closePosition closes pos with a market order the broker makes itself, for the
// given reason, filled at once on this bar at p. A closeout that does not fill
// is an error: the margin rules depend on it.
func (br *broker) closePosition(b bar, act *account, pos *position, why orderReason, p price) error {
	o := &order{
		id:         br.nextID(),
		accountID:  act.id,
		instrument: pos.instrument,
		action:     close,
		orderType:  marketOrder,
		reason:     why,
		price:      p,
		Time:       b.Time,
	}
	o.record(stateRequested, b.Time, "")
	o.record(stateSubmitted, b.Time, "")
	err := br.fill(o, p, b.Time)
	if err == nil || isRejection(err) {
		br.complete(o) // a rejected closeout is terminal too, and kept
	}
	if err != nil {
		return fmt.Errorf("%s %s: %v", why, pos.instrument, err) // %v: never an expected outcome here
	}
	return nil
}

// closeouts is the completed orders the broker made itself to liquidate
// positions, including any that were rejected.
func (br *broker) closeouts() []*order {
	var out []*order
	for _, o := range br.completed {
		if o.reason == reasonMarginCloseout {
			out = append(out, o)
		}
	}
	return out
}

// complete is the one place an order reaches its end: it leaves the queue and
// joins completed, in the order the transitions actually happened. A filled
// close ends the position, so the broker's resting exits for it go with it;
// when one exit fills the other is cancelled here (one cancels the other).
func (br *broker) complete(o *order) {
	br.completed = append(br.completed, o)
	br.orders[o.instrument] = slices.DeleteFunc(br.orders[o.instrument], func(q *order) bool { return q == o })
	if o.action == close && o.state == stateFilled {
		br.cancelWhere(o.Time, "position closed", func(x *order) bool {
			return x.accountID == o.accountID && x.instrument == o.instrument &&
				x.action == close && x.reason != reasonStrategy
		})
	}
}

// processOrder gives a submitted order its chance to execute on bar b.
func (br *broker) processOrder(ctx context.Context, b bar, o *order) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, ok := o.execution(b)
	if !ok {
		return nil
	}
	if err := br.fill(o, p, b.Time); err != nil && !isRejection(err) {
		return err
	}
	br.complete(o) // filled, or rejected by the account
	return nil
}

// execution reports whether the order executes on bar b and at what price.
//
// A market order fills at the open. A resting order waits for the bar to trade
// through its price and fills there, or at the open when the bar gaps past it
// (nothing traded at the price). A strategy's order never fills on the bar
// that produced it; the broker's own exits may, but never on an earlier one.
//
// Which side of the bar triggers a resting order follows from what it does: a
// buy limit and a sell stop wait for the price to fall, a sell limit and a buy
// stop for it to rise. Opening a long buys, closing one sells.
func (o *order) execution(b bar) (price, bool) {
	if o.state != stateSubmitted || o.instrument != b.instrument {
		return 0, false
	}
	if o.reason == reasonStrategy && !b.After(o.Time) || b.Before(o.Time) {
		return 0, false
	}
	switch o.orderType {
	case marketOrder:
		return b.o, true
	case limitOrder, stopOrder:
		// Resting orders use the trigger calculation below.
	default:
		return 0, false
	}
	buys := (o.action == open) == (o.direction == long)
	below := buys == (o.orderType == limitOrder)
	switch {
	case below && b.l <= o.price:
		return min(o.price, b.o), true
	case !below && b.h >= o.price:
		return max(o.price, b.o), true
	}
	return 0, false
}

// fill reports the execution at p, at time at, to the order's account, which validates it
// and updates its balance and positions together. A fill that cannot apply (a
// close with no position, too little margin now) is an expected rejection,
// recorded on the order and returned; anything else is a failure. A filled
// open places its exits.
func (br *broker) fill(o *order, p price, at time.Time) error {
	act, ok := br.accounts[o.accountID]
	if !ok {
		return fmt.Errorf("fill order %s: %w: %s", o.id, errUnknownAccount, o.accountID)
	}
	if err := act.applyFill(o, p, at); err != nil {
		if isRejection(err) {
			return reject(o, at, "%w", err)
		}
		return fmt.Errorf("fill order %s: %w", o.id, err)
	}
	o.fillPrice = p
	o.record(stateFilled, at, "")
	if o.action == open {
		br.placeExits(o)
	}
	return nil
}
