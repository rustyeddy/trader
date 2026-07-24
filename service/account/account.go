// Package account is the service layer between consumers (cmd/account
// today, REST later) and the ./account provider package. Consumers import
// this as accountsvc so the name doesn't collide with ./account.
//
// It owns broker construction (the one place that dispatches a broker
// name to a concrete client) and re-exports just enough of ./account and
// brokers/oanda that callers never need to import either directly.
package account

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers"
	"github.com/rustyeddy/trader/brokers/oanda"
)

// KnownBrokers lists the broker identifiers this build supports.
var KnownBrokers = brokers.KnownBrokers

// IsKnownBroker reports whether name is a supported broker identifier.
func IsKnownBroker(name string) bool {
	return brokers.IsKnownBroker(name)
}

// NewBroker constructs the brokers.Broker for the given broker name and
// env/token. Only "oanda" is real today; extend this switch when a second
// broker arrives.
func NewBroker(name, env, token string) (brokers.Broker, error) {
	switch name {
	case "oanda":
		return oanda.NewClient(env, token)
	default:
		return nil, fmt.Errorf("unknown broker %q (supported: %v)", name, KnownBrokers)
	}
}

// AccountRef and SummaryResult let callers work with ./account and
// brokers/oanda's return types without importing either package.
type (
	AccountRef    = oanda.AccountRef
	SummaryResult = account.AccountSummaryResult
	OpenTrade     = oanda.OpenTrade
	AccountCfg    = account.AccountCfg
)

// AccountSummaries returns a summary result for each ID in accountIDs. If
// accountIDs is empty, every account the broker's token can see is
// summarized instead.
func AccountSummaries(ctx context.Context, broker brokers.Broker, accountIDs []string) ([]SummaryResult, error) {
	return account.GetAccountSummary(ctx, broker, accountIDs)
}

// OpenTrades returns the open trades on accountID, fetched directly through
// the broker interface (no session/snapshot cache).
func OpenTrades(ctx context.Context, broker brokers.Broker, accountID string) ([]OpenTrade, error) {
	return account.GetOpenTrades(ctx, broker, accountID)
}

// Resolve returns the cached session for the given account ID — see
// ./account's process-wide session cache — creating it on first use.
func Resolve(ctx context.Context, id string, client *oanda.Client, log *slog.Logger) (*account.Account, error) {
	return account.Resolve(ctx, id, client, log)
}

// ResolveFirst returns defaultID's session if set, otherwise the first
// account the client's token can see.
func ResolveFirst(ctx context.Context, defaultID string, client *oanda.Client, log *slog.Logger) (*account.Account, error) {
	return account.ResolveFirst(ctx, defaultID, client, log)
}

// ResolveTarget resolves the broker and account ID a command should operate
// on from flag values, the OANDA_ACCOUNT_ID env var, a caller-supplied
// config default, and the CLI's locally persisted "active" selection. See
// account.ResolveTarget for the full priority order.
func ResolveTarget(brokerFlag string, brokerChanged bool, accountIDFlag string, accountIDChanged bool, configAccountID string) (resolvedBroker, resolvedAccountID string, err error) {
	return account.ResolveTarget(brokerFlag, brokerChanged, accountIDFlag, accountIDChanged, configAccountID)
}

// ResolveAccountID returns accountID if non-empty, otherwise queries the
// broker's token for its accounts: exactly one -> use it; none -> error;
// more than one -> AmbiguousAccountError listing the candidate IDs.
// CLI-only — REST/MCP must always supply an explicit account ID, never
// resolve a default this way.
func ResolveAccountID(ctx context.Context, broker brokers.Broker, accountID string) (string, error) {
	if accountID != "" {
		return accountID, nil
	}
	refs, err := broker.GetAccounts(ctx)
	if err != nil {
		return "", fmt.Errorf("discover accounts: %w", err)
	}
	if len(refs) == 0 {
		return "", fmt.Errorf("no accounts found for this token")
	}
	if len(refs) > 1 {
		ids := make([]string, len(refs))
		for i, r := range refs {
			ids[i] = r.ID
		}
		return "", AmbiguousAccountError{Accounts: ids}
	}
	return refs[0].ID, nil
}

// AmbiguousAccountError is returned by ResolveAccountID when the token has
// access to multiple accounts and none was specified. Callers should
// display the candidate IDs and ask the user to pick one.
type AmbiguousAccountError struct {
	Accounts []string
}

func (e AmbiguousAccountError) Error() string {
	return fmt.Sprintf("ambiguous account: multiple accounts available, specify one (%s)", strings.Join(e.Accounts, ", "))
}

// DefaultAccountID picks the default account ID out of refs: configured if
// non-empty (a caller-supplied default, e.g. server config), otherwise the
// first ref, otherwise "" if refs is empty.
func DefaultAccountID(refs []AccountRef, configured string) string {
	if configured != "" {
		return configured
	}
	if len(refs) > 0 {
		return refs[0].ID
	}
	return ""
}
