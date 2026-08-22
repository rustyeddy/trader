package broker

import (
	"context"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/id"
)

// Broker is a broker/session-level port: identity, account discovery,
// and opening an account-scoped operational handle. Every adapter
// (the deterministic simulator, and future real broker adapters)
// implements Broker as its entry point; see ADR-008.
type Broker interface {
	// Name identifies which broker or provider this is, for example
	// "OANDA" or "sim". It should match the Broker field carried by
	// account.Reference and account.Snapshot values this Broker
	// produces.
	Name() string

	// Accounts lists the accounts this broker session can open,
	// without opening any of them or performing any account-scoped
	// query. Implementations must not return a Reference whose Broker
	// does not equal Name().
	Accounts(ctx context.Context) ([]account.Reference, error)

	// OpenAccount returns an operational handle for the account
	// identified by accountID. It returns an error satisfying
	// errors.Is(err, ErrAccountNotFound) if accountID does not name an
	// account this Broker can open.
	OpenAccount(ctx context.Context, accountID id.AccountID) (Account, error)

	// Close releases any resources this Broker holds — connections,
	// sessions, or background work. A Broker must not be used after
	// Close returns; every Account handle it opened becomes invalid
	// too.
	Close() error
}
