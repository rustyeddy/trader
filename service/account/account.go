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
)

// ListAccounts returns every account ID the broker's token can see.
func ListAccounts(ctx context.Context, broker brokers.Broker) ([]AccountRef, error) {
	return account.GetAccounts(ctx, broker)
}

// AccountSummaries returns a summary result for each ID in accountIDs. If
// accountIDs is empty, every account the broker's token can see is
// summarized instead.
func AccountSummaries(ctx context.Context, broker brokers.Broker, accountIDs []string) ([]SummaryResult, error) {
	return account.GetAccountSummary(ctx, broker, accountIDs)
}
