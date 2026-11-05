package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
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

	// closedOut is the bar time of each account's latest margin closeout. A
	// closeout is not undone by a new open made on the same observation; later
	// re-entry rules are a future policy.
	closedOut map[string]time.Time

	// completed keeps every order that has reached a terminal state (filled,
	// rejected by the broker or cancelled), in the order it happened, for the
	// journal. Only complete appends to it. The broker's own liquidations are in
	// it too; closeouts picks them out by their reason.
	completed []*order
}

var brokers map[string]*broker

func init() {
	brokers = make(map[string]*broker)
	brokers["sim"] = &broker{id: "sim"}
	brokers["sim"].addAccount("sim123", usd(10000))
}

func getBroker(id string) (br *broker, ok bool) {
	if br, ok = brokers[id]; !ok {
		brokers[id] = &broker{id: id, accounts: make(map[string]*account)}
		br = brokers[id]
	}
	return br, true
}

func (br *broker) addAccount(id string, balance money) *account {
	if br.accounts == nil {
		br.accounts = make(map[string]*account)
	}
	act := newAccount(id, balance)
	br.accounts[id] = act
	return act
}

func (br *broker) getAccount(id string) (*account, bool) {
	a, ok := br.accounts[id]
	return a, ok
}

// submit has the order's account size it, assigns it an id and queues it to
// await execution. The order is filled (or rejected) on a later bar by onBar;
// submit itself never fills. An expected rejection from sizing is returned for
// the caller to classify; the order has already recorded it.
func (br *broker) submit(ctx context.Context, o *order) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.accountID == "" {
		return fmt.Errorf("submit to broker %s: %w: the order has no account", br.id, errUnknownAccount)
	}
	act, ok := br.getAccount(o.accountID)
	if !ok {
		return fmt.Errorf("submit to broker %s: %w: %s", br.id, errUnknownAccount, o.accountID)
	}

	if o.action == open || o.action == add {
		if at, ok := br.closedOut[o.accountID]; ok && !o.Time.After(at) {
			return fmt.Errorf("submit to broker %s: %w: account %s at %s",
				br.id, errCloseoutGuard, o.accountID, at.Format(time.DateOnly))
		}
	}

	if o.action == open && br.hasPendingOpen(o.accountID, o.instrument) {
		return fmt.Errorf("submit to broker %s: %w: account %s on %s",
			br.id, errOpenPending, o.accountID, o.instrument)
	}

	reserved, err := br.reservedMargin(o.accountID)
	if err != nil {
		return fmt.Errorf("submit to broker %s: %w", br.id, err)
	}
	if err := act.size(o, reserved); err != nil {
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
	o.record(stateSubmitted, "")
	if br.orders == nil {
		br.orders = make(map[string][]*order)
	}
	br.orders[o.instrument] = append(br.orders[o.instrument], o)
	return nil
}

// hasPendingOpen reports whether the account has a submitted open waiting on the
// instrument.
func (br *broker) hasPendingOpen(accountID, instrument string) bool {
	for _, o := range br.orders[instrument] {
		if o.accountID == accountID && o.state == stateSubmitted && o.action == open {
			return true
		}
	}
	return false
}

// nextID issues the next order id.
func (br *broker) nextID() string {
	br.seq++
	return fmt.Sprintf("%s-%d", br.id, br.seq)
}

// reservedMargin is the margin held by the account's submitted opens that have
// not filled yet, at each order's reference price. Closes reserve nothing.
func (br *broker) reservedMargin(accountID string) (money, error) {
	var total money
	for _, orders := range br.orders {
		for _, o := range orders {
			if o.accountID != accountID || o.state != stateSubmitted || o.action != open {
				continue
			}
			m, err := marginFor(o.instrument, o.units, o.price)
			if err != nil {
				return 0, err
			}
			if total, err = checkedAdd(total, m); err != nil {
				return 0, err
			}
		}
	}
	return total, nil
}

// phases is how many passes onBar makes over an instrument's queue; see phase.
const phases = 5

// phase is the pass of a bar in which the order is considered: market orders
// first (they fill at the open), then limit entries, stop entries, protective
// stops and last take profits: a bar that reaches both an exit's stop and its
// target is taken as the stop (the pessimistic path), which then cancels the
// target.
func (o *order) phase() int {
	switch {
	case o.orderType == marketOrder:
		return 0
	case o.orderType == limitOrder && o.action == open:
		return 1
	case o.orderType == stopOrder && o.action == open:
		return 2
	case o.orderType == limitOrder: // a take profit: limit entries returned above
		return 4
	}
	return 3
}

// onBar executes the orders waiting on b's instrument for every account, then
// values each account's positions at the bar's close. The trader calls it at
// the start of every bar, before the strategy sees the bar, so an order
// submitted on bar N can first fill on bar N+1 and never on the bar that
// produced it. Orders that are no longer submitted (filled or rejected) leave
// the queue.
func (br *broker) onBar(ctx context.Context, b bar) error {
	// Market orders fill at the open, so they go first; limit and stop entries
	// then fill on the rest of the bar's range, and the protective stops last.
	// A stop placed by an entry filled above is live on this bar too.
	//
	// Assumption: a bar gives only open, high, low and close, not the path
	// between them. Within a bar we assume entries happen before protective
	// stops, and that the stop then sees the bar's whole range. That is exact
	// for a market entry (it fills at the open, before everything else), but
	// for a limit or stop entry it is pessimistic when the extreme that hits the
	// stop came before the entry (open, low, high, close for a long's stop). It
	// can overstate stop-outs on entry bars; see the todo for options.
	for phase := 0; phase < phases; phase++ {
		for _, o := range append([]*order(nil), br.orders[b.instrument]...) {
			if o.phase() != phase {
				continue
			}
			if err := br.processOrder(ctx, b, o); err != nil {
				return err
			}
		}
	}
	br.prune(b.instrument)

	ids := make([]string, 0, len(br.accounts))
	for id := range br.accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, act := range br.accounts {
		act.mark(b)
	}
	for _, id := range ids {
		if err := br.enforceMargin(ctx, b, br.accounts[id]); err != nil {
			return fmt.Errorf("margin check %s: %w", id, err)
		}
	}
	return nil
}

// prune drops the instrument's orders that are no longer submitted from the
// queue. It does not record them: complete does that when they finish.
func (br *broker) prune(instrument string) {
	if len(br.orders[instrument]) == 0 {
		return // nothing queued; never write to a nil map
	}
	var waiting []*order
	for _, o := range br.orders[instrument] {
		if o.state == stateSubmitted {
			waiting = append(waiting, o)
		}
	}
	br.orders[instrument] = waiting
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
	br.cancelOpens(act.id, b.Time, "margin closeout")
	if br.closedOut == nil {
		br.closedOut = make(map[string]time.Time)
	}
	br.closedOut[act.id] = b.Time
	for status == marginLiquidate || br.completeCloseout {
		pos, err := pick(act)
		if err != nil {
			return err
		}
		if pos == nil {
			return nil // nothing left to close
		}
		if err := br.closeoutPosition(b, act, pos); err != nil {
			return err
		}
		if status, err = policy(act); err != nil {
			return err
		}
	}
	return nil
}

// cancelOpens cancels the account's submitted opens: once the account is being
// liquidated they must not fill and take the margin back.
func (br *broker) cancelOpens(accountID string, at time.Time, reason string) {
	for instrument, orders := range br.orders {
		cancelled := false
		for _, o := range orders {
			if o.accountID == accountID && o.state == stateSubmitted && o.action == open {
				o.Time = at // the cancellation happens now, not when the order was made
				o.record(stateCancelled, reason)
				br.complete(o)
				cancelled = true
			}
		}
		if cancelled {
			br.prune(instrument)
		}
	}
}

// throughPrice reports whether bar b trades through price p against a
// position of direction dir (the low reaches it for a long, the high for a
// short) and, if so, the price an order resting at p fills at: p itself, or the
// bar's open when it gaps beyond p (nothing traded at p). A long's buy limit
// and a long's sell stop both trigger downward, a short's upward, so entry
// limits and protective stops share it. For a limit the gap fill is the better
// price; for a stop it is the worse one.
func throughPrice(dir direction, p price, b bar) (price, bool) {
	switch dir {
	case long:
		if b.l <= p {
			return min(p, b.o), true
		}
	case short:
		if b.h >= p {
			return max(p, b.o), true
		}
	}
	return 0, false
}

// placeStop queues the protective stop for the position the filled open o
// created: a resting close order the broker owns, like any other order waiting
// on a trigger. An open without a stop gets none.
func (br *broker) placeStop(o *order) {
	if o.stopLoss > 0 {
		br.placeExit(o, stopOrder, reasonStopLoss, o.stopLoss)
	}
}

// placeTarget queues the take-profit for the position the filled open o
// created: a resting limit close. An open without a target gets none.
func (br *broker) placeTarget(o *order) {
	if o.takeProfit > 0 {
		br.placeExit(o, limitOrder, reasonTakeProfit, o.takeProfit)
	}
}

// placeExit queues a resting close for the position the filled open o created.
func (br *broker) placeExit(o *order, typ orderType, why orderReason, p price) {
	s := &order{
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
	s.record(stateRequested, "")
	s.record(stateSubmitted, "")
	if br.orders == nil {
		br.orders = make(map[string][]*order)
	}
	br.orders[s.instrument] = append(br.orders[s.instrument], s)
}

// cancelExits cancels the account's resting stop and target on an instrument
// once its position is gone: they must never close a later position. When one
// exit fills the other is cancelled here, so the pair acts as one-cancels-other.
func (br *broker) cancelExits(accountID, instrument string, at time.Time) {
	for _, s := range br.orders[instrument] {
		if s.accountID == accountID && s.state == stateSubmitted &&
			(s.reason == reasonStopLoss || s.reason == reasonTakeProfit) {
			s.Time = at
			s.record(stateCancelled, "position closed")
			br.complete(s)
		}
	}
	br.prune(instrument)
}

// finish records an order that reached a terminal state. A filled close ends
// the position, so its resting exits go with it.
func (br *broker) finish(o *order) {
	br.complete(o)
	if o.action == close && o.state == stateFilled {
		br.cancelExits(o.accountID, o.instrument, o.Time)
	}
}

// closeoutPosition liquidates pos at its latest mark.
func (br *broker) closeoutPosition(b bar, act *account, pos *position) error {
	return br.closePosition(b, act, pos, reasonMarginCloseout, pos.markPrice)
}

// closePosition closes pos with a market order the broker makes itself, for the
// given reason, filled at once on this bar at p.
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
	o.record(stateRequested, "")
	o.record(stateSubmitted, "")
	if err := br.fillOrder(o, p); err != nil {
		return err // not terminal: the run stops and the error is logged
	}
	br.finish(o)
	if o.state != stateFilled {
		return fmt.Errorf("%s %s: the order was %s", why, pos.instrument, o.state)
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

// complete is the one place a terminal order is recorded as completed, so
// completed is in the order the transitions actually happened, whatever the
// queue order or a later cleanup.
func (br *broker) complete(o *order) {
	br.completed = append(br.completed, o)
}

// processOrder gives a submitted order its chance to execute on bar b.
func (br *broker) processOrder(ctx context.Context, b bar, o *order) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !br.shouldExecute(o, b) {
		return nil
	}

	o.Time = b.Time // the order is now on the fill bar
	if err := br.fillOrder(o, executionPrice(o, b)); err != nil {
		return err
	}
	br.finish(o) // filled, or rejected by the account
	return nil
}

// shouldExecute reports whether the submitted order executes on bar b. A
// market order executes on a bar after the one that produced it. Limit and stop
// entries also wait for a later bar, then fill when the bar trades through
// their price. A protective stop fills when the bar trades through its price,
// including the bar it was placed on, but never against an earlier bar.
func (br *broker) shouldExecute(o *order, b bar) bool {
	if o.state != stateSubmitted || o.instrument != b.instrument {
		return false
	}
	switch o.orderType {
	case marketOrder:
		return b.Time.After(o.Time)
	case limitOrder, stopOrder:
		if o.action == open && !b.Time.After(o.Time) {
			return false // an entry never fills on the bar that produced it
		}
		if b.Time.Before(o.Time) {
			return false // never against an observation older than the order
		}
		_, hit := o.trigger(b)
		return hit
	}
	return false
}

// trigger reports whether bar b trades through the order's price and the price
// it fills at. A limit entry and a protective stop trigger against the
// direction of the position they open or protect: a long's buy limit and sell
// stop wait for the price to fall. A stop entry buys or sells into the move, so
// it triggers the other way: a long's buy stop waits for the price to rise.
func (o *order) trigger(b bar) (price, bool) {
	dir := o.direction
	// A stop entry and a take profit run against the grain of the other
	// orders: they trigger the way the position they open or close profits.
	if (o.orderType == stopOrder && o.action == open) || (o.orderType == limitOrder && o.action == close) {
		switch dir {
		case long:
			dir = short
		case short:
			dir = long
		}
	}
	return throughPrice(dir, o.price, b)
}

// executionPrice is the price the order fills at on bar b: a market order
// fills at the bar's open, the first price after the signal; a limit or stop
// fills at its price, or the open when the bar gapped past it.
func executionPrice(o *order, b bar) price {
	if o.orderType == stopOrder || o.orderType == limitOrder {
		p, _ := o.trigger(b)
		return p
	}
	return b.o
}

// fillOrder reports the execution at p to the order's account, which validates
// it and updates its balance and positions together. A fill that
// cannot apply (a close with no position) is an expected rejection, recorded
// on the order; anything else is returned and stops the run.
func (br *broker) fillOrder(o *order, p price) error {
	act, ok := br.getAccount(o.accountID)
	if !ok {
		return fmt.Errorf("fill order %s: %w: %s", o.id, errUnknownAccount, o.accountID)
	}

	o.fillPrice = p
	if err := act.applyFill(o); err != nil {
		if !isRejection(err) {
			return fmt.Errorf("fill order %s: %w", o.id, err)
		}
		o.fillPrice = 0 // a rejected order never filled
		o.record(stateRejected, err.Error())
		slog.Warn("order rejected",
			"reason", err,
			"time", o.Time,
			"instrument", o.instrument,
			"action", o.action,
			"direction", o.direction)
		return nil
	}
	o.record(stateFilled, "")
	if o.action == open {
		br.placeStop(o)
		br.placeTarget(o)
	}
	return nil
}
