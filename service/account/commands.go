package account

import (
	"context"
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers"
)

// newDefaultBroker builds the broker client for callers that supply only an
// AccountCfg: always "practice" env, token resolved from
// OANDA_TOKEN/~/.config/oanda/pat.txt (see oanda.ResolveToken).
func newDefaultBroker(name string) (brokers.Broker, error) {
	return NewBroker(name, "practice", "")
}

// ListResult is List's return value.
type ListResult struct {
	Accounts         []AccountRef
	DefaultAccountID string // marks the default in Accounts, "" if none
}

// List resolves cfg, builds its own broker, and returns every account ID
// the broker's token can see.
func List(ctx context.Context, cfg AccountCfg) (ListResult, error) {
	targetBroker, defaultAccountID, err := ResolveTarget(cfg.Broker, cfg.BrokerChanged, cfg.AccountID, cfg.AccountIDChanged, "")
	if err != nil {
		return ListResult{}, err
	}
	b, err := newDefaultBroker(targetBroker)
	if err != nil {
		return ListResult{}, err
	}
	accounts, err := account.GetAccounts(ctx, b)
	if err != nil {
		return ListResult{}, fmt.Errorf("list accounts: %w", err)
	}
	return ListResult{Accounts: accounts, DefaultAccountID: defaultAccountID}, nil
}

// Summary resolves cfg, builds its own broker, and returns a summary for
// cfg.AccountID (if given), otherwise every account the broker's token can
// see.
func Summary(ctx context.Context, cfg AccountCfg) ([]SummaryResult, error) {
	targetBroker, _, err := ResolveTarget(cfg.Broker, cfg.BrokerChanged, cfg.AccountID, cfg.AccountIDChanged, "")
	if err != nil {
		return nil, err
	}
	b, err := newDefaultBroker(targetBroker)
	if err != nil {
		return nil, err
	}
	var ids []string
	if cfg.AccountIDChanged {
		ids = []string{cfg.AccountID}
	}
	results, err := AccountSummaries(ctx, b, ids)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	return results, nil
}

// Orders resolves cfg, builds its own broker, and returns the open trades
// on the resolved account.
func Orders(ctx context.Context, cfg AccountCfg) ([]OpenTrade, error) {
	targetBroker, resolvedAccountID, err := ResolveTarget(cfg.Broker, cfg.BrokerChanged, cfg.AccountID, cfg.AccountIDChanged, "")
	if err != nil {
		return nil, err
	}
	b, err := newDefaultBroker(targetBroker)
	if err != nil {
		return nil, err
	}
	trades, err := OpenTrades(ctx, b, resolvedAccountID)
	if err != nil {
		return nil, fmt.Errorf("list open trades: %w", err)
	}
	return trades, nil
}

// DefaultResult is Default's return value.
type DefaultResult struct {
	Selection    Selection
	HasSelection bool // false when getting and nothing is persisted
	DidSet       bool // true when this call set the selection (vs. got it)
}

// Default gets the CLI's persisted default broker/account when neither
// cfg.BrokerChanged nor cfg.AccountIDChanged, otherwise sets it from
// cfg.Broker/cfg.AccountID (both must be changed together).
func Default(cfg AccountCfg) (DefaultResult, error) {
	if !cfg.BrokerChanged && !cfg.AccountIDChanged {
		sel, err := DefaultSelection()
		if err != nil {
			return DefaultResult{}, err
		}
		return DefaultResult{Selection: sel, HasSelection: !sel.IsZero()}, nil
	}

	if cfg.BrokerChanged != cfg.AccountIDChanged {
		return DefaultResult{}, fmt.Errorf("--broker and --account-id must be set together")
	}
	if !IsKnownBroker(cfg.Broker) {
		return DefaultResult{}, fmt.Errorf("unknown broker %q (supported: %s)", cfg.Broker, strings.Join(KnownBrokers, ", "))
	}
	if err := SetDefault(cfg.Broker, cfg.AccountID); err != nil {
		return DefaultResult{}, err
	}
	return DefaultResult{Selection: Selection{Broker: cfg.Broker, AccountID: cfg.AccountID}, DidSet: true}, nil
}
