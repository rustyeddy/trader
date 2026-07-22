package account

import (
	"context"
	"time"

	"github.com/rustyeddy/trader/brokers"
)

// EnsureSnapshot starts the account's background changes-poll goroutine if it
// is not already running. The goroutine binds to ctx, so it stops when ctx is
// cancelled. Safe to call from multiple goroutines; only one goroutine is ever
// started per Account.
func (acct *Account) EnsureSnapshot(ctx context.Context, interval time.Duration) {
	acct.snapMu.Lock()
	if acct.snapshot == nil {
		acct.snapshot = newAccountSnapshot(acct.broker(), acct.ID, acct.Log)
	}
	snap := acct.snapshot
	acct.snapMu.Unlock()

	if !snap.IsRunning() {
		if err := snap.Start(ctx, interval); err != nil {
			if acct.Log != nil {
				acct.Log.Warn("account snapshot: start failed", "err", err, "account", acct.ID)
			}
		}
	}
}

// getSnapshot returns the running AccountSnapshot for this account, or nil if
// no snapshot is running. Callers fall back to direct OANDA calls when nil.
func (acct *Account) getSnapshot() *AccountSnapshot {
	acct.snapMu.RLock()
	s := acct.snapshot
	acct.snapMu.RUnlock()
	if s != nil && s.IsRunning() {
		return s
	}
	return nil
}

// broker narrows acct.OANDA to the brokers.Broker execution interface.
// *oanda.Client satisfies Broker unchanged (see brokers/broker.go). Only
// execution methods (order placement, close, stop, account summary) go
// through here; pricing/candle calls stay on acct.OANDA directly, since
// those are DataProvider-shaped and out of scope for this phase.
func (acct *Account) broker() brokers.Broker {
	return acct.OANDA
}
