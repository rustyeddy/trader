package account

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/brokers/oanda"
)

// GetAccountSummary returns balance, NAV, margin, and unrealized P/L.
// When the account snapshot is running it reads from the local cache;
// otherwise it falls back to a direct OANDA REST call.
func (acct *Account) GetAccountSummary(ctx context.Context) (*oanda.AccountSummary, error) {
	if snap := acct.getSnapshot(); snap != nil {
		return snap.Summary(), nil
	}
	summary, err := acct.broker().GetAccountSummary(ctx, acct.ID)
	if err != nil {
		return nil, fmt.Errorf("get account summary: %w", err)
	}
	return summary, nil
}

// GetTransactions polls for transactions with ID > sinceID. Returns the
// transactions and the new lastTransactionID for the next poll.
//
// OANDA caps responses at 1000; if you get back exactly 1000, call again
// with the new lastID.
func (acct *Account) GetTransactions(ctx context.Context, sinceID int64) ([]oanda.Transaction, int64, error) {
	return acct.broker().GetTransactions(ctx, acct.ID, sinceID)
}

// StreamTransactions opens a push subscription to the OANDA transaction
// stream. The returned channel closes when ctx is cancelled or the stream
// errors out (final event carries non-nil Err in the error case).
func (acct *Account) StreamTransactions(ctx context.Context, opts oanda.StreamOptions) (<-chan oanda.TxEvent, error) {
	return acct.broker().StreamTransactions(ctx, acct.ID, opts)
}
