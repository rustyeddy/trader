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

// onBar executes the orders waiting on b's instrument for every account, then
// values each account's positions at the bar's close. The trader calls it at
// the start of every bar, before the strategy sees the bar, so an order
// submitted on bar N can first fill on bar N+1 and never on the bar that
// produced it. Orders that are no longer submitted (filled or rejected) leave
// the queue.
func (br *broker) onBar(ctx context.Context, b bar) error {
	pending := append([]*order(nil), br.orders[b.instrument]...)

	for _, o := range pending {
		if err := br.processOrder(ctx, b, o); err != nil {
			return err
		}
	}

	if len(pending) > 0 {
		br.prune(b.instrument)
	}
	for _, act := range br.accounts {
		act.mark(b)
	}

	// Judge every account against the margin rules, in a fixed order.
	ids := make([]string, 0, len(br.accounts))
	for id := range br.accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := br.enforceMargin(ctx, b, br.accounts[id]); err != nil {
			return fmt.Errorf("margin check %s: %w", id, err)
		}
	}
	return nil
}

// prune drops the instrument's orders that are no longer submitted.
func (br *broker) prune(instrument string) {
	waiting := br.orders[instrument][:0]
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
	br.cancelOpens(act.id, "cancelled by margin closeout")
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

// cancelOpens rejects the account's submitted opens: once the account is being
// liquidated they must not fill and take the margin back.
func (br *broker) cancelOpens(accountID, reason string) {
	for instrument, orders := range br.orders {
		cancelled := false
		for _, o := range orders {
			if o.accountID == accountID && o.state == stateSubmitted && o.action == open {
				o.record(stateRejected, reason)
				cancelled = true
			}
		}
		if cancelled {
			br.prune(instrument)
		}
	}
}

// closeoutPosition closes pos at its latest mark with a market order the
// broker makes itself. It fills at once, on this bar.
func (br *broker) closeoutPosition(b bar, act *account, pos *position) error {
	o := &order{
		id:         br.nextID(),
		accountID:  act.id,
		instrument: pos.instrument,
		action:     close,
		orderType:  marketOrder,
		reason:     reasonMarginCloseout,
		price:      pos.markPrice,
		Time:       b.Time,
	}
	o.record(stateRequested, "")
	o.record(stateSubmitted, "")
	if err := br.fillOrder(o, pos.markPrice); err != nil {
		return err
	}
	if o.state != stateFilled {
		return fmt.Errorf("closeout %s: the order was %s", pos.instrument, o.state)
	}
	return nil
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
	return br.fillOrder(o, executionPrice(o, b))
}

// shouldExecute reports whether the submitted order executes on bar b. Only
// market orders execute so far, and only on a bar after the one that produced
// them; limit and stop orders stay submitted until their trigger rules exist.
func (br *broker) shouldExecute(o *order, b bar) bool {
	return o.state == stateSubmitted &&
		o.orderType == marketOrder &&
		o.instrument == b.instrument &&
		b.Time.After(o.Time)
}

// executionPrice is the price the order fills at on bar b: a market order
// fills at the bar's open, the first price after the signal.
func executionPrice(o *order, b bar) price {
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
	return nil
}
