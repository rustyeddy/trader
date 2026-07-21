package sim

import (
	"context"

	"github.com/rustyeddy/trader/brokers/oanda"
)

// Minimal/no-op brokers.Broker methods: nothing exercises Sim through these
// yet. They matter for reconciliation-against-a-real-broker (see
// docs/Manual/architecture-broker-account-order.org, phase 4 chunk 4),
// which is OANDA-specific by definition and doesn't apply the same way to
// a self-authoritative sim — Sim's own state is already the source of
// truth, nothing to reconcile against. Kept in a separate file so this
// scope boundary is visible at a glance, not buried among the real
// implementations in simulator.go.

// GetAccountChanges has nothing to report: Sim has no external authority
// to have "changed" independently of the calls made through it.
func (e *Sim) GetAccountChanges(ctx context.Context, accountID string, sinceID int64) (*oanda.AccountChangesResult, error) {
	return &oanda.AccountChangesResult{}, nil
}

// GetAccounts returns the one account Sim wraps.
func (e *Sim) GetAccounts(ctx context.Context) ([]oanda.AccountRef, error) {
	if e == nil || e.account == nil {
		return nil, nil
	}
	return []oanda.AccountRef{{ID: e.account.ID}}, nil
}

// GetTransactions returns no transaction history — Sim doesn't keep one
// yet (see GetAccountChanges).
func (e *Sim) GetTransactions(ctx context.Context, accountID string, sinceID int64) ([]oanda.Transaction, int64, error) {
	return nil, sinceID, nil
}

// StreamTransactions returns a closed channel — Sim has no push feed;
// callers that need live updates should poll GetOpenTrades/GetAccountSummary.
func (e *Sim) StreamTransactions(ctx context.Context, accountID string, opts oanda.StreamOptions) (<-chan oanda.TxEvent, error) {
	ch := make(chan oanda.TxEvent)
	close(ch)
	return ch, nil
}
