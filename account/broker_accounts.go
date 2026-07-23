package account

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/brokers"
	"github.com/rustyeddy/trader/brokers/oanda"
)

// GetAccounts returns every account ID the broker's token can see.
func GetAccounts(ctx context.Context, broker brokers.Broker) ([]oanda.AccountRef, error) {
	if broker == nil {
		return nil, fmt.Errorf("account: broker not configured")
	}
	refs, err := broker.GetAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("get accounts: %w", err)
	}
	return refs, nil
}

// AccountSummaryResult pairs one account ID with its summary or the error
// hit fetching it. GetAccountSummary tolerates per-account failures
// instead of aborting the whole batch.
type AccountSummaryResult struct {
	ID      string
	Summary *oanda.AccountSummary
	Err     error
}

// GetAccountSummary returns a summary result for each ID in accountIDs. If
// accountIDs is empty, every account the broker's token can see is
// summarized instead.
func GetAccountSummary(ctx context.Context, broker brokers.Broker, accountIDs []string) ([]AccountSummaryResult, error) {
	if broker == nil {
		return nil, fmt.Errorf("account: broker not configured")
	}

	ids := accountIDs
	if len(ids) == 0 {
		refs, err := broker.GetAccounts(ctx)
		if err != nil {
			return nil, fmt.Errorf("list accounts: %w", err)
		}
		ids = make([]string, len(refs))
		for i, ref := range refs {
			ids[i] = ref.ID
		}
	}

	out := make([]AccountSummaryResult, len(ids))
	for i, id := range ids {
		s, err := broker.GetAccountSummary(ctx, id)
		out[i] = AccountSummaryResult{ID: id, Summary: s, Err: err}
	}
	return out, nil
}

// GetOpenTrades returns the open trades on accountID via the broker
// interface directly (no session/snapshot cache — see (*Account).ListOpenTrades
// for the cached variant used by the live runner).
func GetOpenTrades(ctx context.Context, broker brokers.Broker, accountID string) ([]oanda.OpenTrade, error) {
	if broker == nil {
		return nil, fmt.Errorf("account: broker not configured")
	}
	trades, err := broker.GetOpenTrades(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get open trades: %w", err)
	}
	return trades, nil
}
