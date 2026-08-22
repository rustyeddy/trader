package broker

import (
	"context"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/order"
)

// Account is an account-scoped operational handle: actions and queries
// against one broker account, obtained from Broker.OpenAccount. It is
// distinct from account.Snapshot (ADR-007): Snapshot is an immutable
// value describing observed state with no methods that perform I/O,
// while Account performs the I/O that produces one.
//
// Every method returns canonical account or order package values —
// never a provider-native or vendor SDK type. See the package doc
// comment for capabilities (event streaming, capability discovery)
// deliberately not part of this core contract.
type Account interface {
	// Reference identifies which account this handle operates on.
	Reference() account.Reference

	// Snapshot returns this account's current observed state.
	Snapshot(ctx context.Context) (account.Snapshot, error)

	// Submit asks the broker to accept req as a new order, returning
	// the resulting Order in whatever state the broker reports
	// synchronously — for example StatusPendingSubmit or
	// StatusWorking. Later lifecycle changes are expected to arrive
	// through the event contract issue #147 (M3-04) defines, not
	// through Submit's return value.
	Submit(ctx context.Context, req order.Request) (order.Order, error)

	// Cancel asks the broker to cancel an existing order. It returns an
	// error satisfying errors.Is(err, ErrOrderNotFound) if req.OrderID
	// does not name an order this account knows about — distinct from
	// ErrAccountNotFound, which means the account handle itself could
	// not be opened.
	Cancel(ctx context.Context, req order.CancelRequest) (order.CancelResult, error)

	// Replace asks the broker to modify an existing order's quantity
	// and/or prices in place. It returns an error satisfying
	// errors.Is(err, ErrOrderNotFound) if req.OrderID does not name an
	// order this account knows about.
	Replace(ctx context.Context, req order.ReplaceRequest) (order.ReplaceResult, error)
}
