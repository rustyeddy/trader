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

func (b *broker) addAccount(id string, balance money) *account {
	if b.accounts == nil {
		b.accounts = make(map[string]*account)
	}
	act := newAccount(id, balance)
	b.accounts[id] = act
	return act
}

func (b *broker) getAccount(id string) (*account, bool) {
	a, ok := b.accounts[id]
	return a, ok
}

// submit accepts a sized order from the named account. For now the broker only
// assigns the order an id, marks it submitted and logs the request; it does
// not fill or reject it.
func (b *broker) submit(ctx context.Context, accountID string, o *order) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := b.getAccount(accountID); !ok {
		return fmt.Errorf("submit to broker %s: %w: %s", b.id, errUnknownAccount, accountID)
	}

	if o.id == "" {
		b.seq++
		o.id = fmt.Sprintf("%s-%d", b.id, b.seq)
	}

	slog.InfoContext(ctx, "order submitted",
		"broker", b.id,
		"account", accountID,
		"order", o.id,
		"instrument", o.instrument,
		"action", o.action,
		"direction", o.direction,
		"type", o.orderType,
		"units", o.units)
	o.record(stateSubmitted, "")
	return nil
}
