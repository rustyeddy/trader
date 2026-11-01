package main

import (
	"context"
	"fmt"
	"log/slog"
)

type broker struct {
	id       string
	accounts map[string]*account
	seq      int // last order id issued
}

var brokers map[string]*broker

func init() {
	brokers = make(map[string]*broker)
	brokers["sim"] = &broker{id: "sim"}
	brokers["sim"].addAccount("sim123", 10000)
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
	act.broker = br
	br.accounts[id] = act
	return act
}

func (br *broker) getAccount(id string) (*account, bool) {
	a, ok := br.accounts[id]
	return a, ok
}

// submit accepts a sized order from the account named in o.accountID, assigns
// it an id and hands it to the account to await execution. The order is filled
// (or rejected) on a later bar by processOrder; submit itself never fills.
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

	if o.id == "" {
		br.seq++
		o.id = fmt.Sprintf("%s-%d", br.id, br.seq)
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
	act.orders[o.instrument] = append(act.orders[o.instrument], o)
	return nil
}

// processOrder gives a submitted order its chance to execute on bar b. The
// account calls it at the start of every bar, before the strategy sees the bar,
// so an order submitted on bar N can first fill on bar N+1 and never on the bar
// that produced it.
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

// fillOrder applies the order to its account's positions at p. A fill that
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
