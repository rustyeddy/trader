package account

import (
	"context"
	"log/slog"

	"github.com/rustyeddy/trader/brokers/oanda"
)

// shared is the process-wide cache of live account sessions. Every
// consumer (CLI, REST, MCP, ...) that resolves the same account ID within
// one process gets back the same *Account — and therefore the same
// running snapshot poller, if one was started for it. Caching lives here,
// in the provider, never in a caller-owned struct (Service or otherwise).
var shared Registry

// Resolve returns the session for the given account ID, creating and
// caching it in the shared process-wide cache on first use.
func Resolve(ctx context.Context, id string, client *oanda.Client, log *slog.Logger) (*Account, error) {
	return shared.Account(ctx, id, client, log)
}

// ResolveAll returns a session for every account the client's token can access.
func ResolveAll(ctx context.Context, client *oanda.Client, log *slog.Logger) ([]*Account, error) {
	return shared.Accounts(ctx, client, log)
}

// ResolveFirst returns defaultID's session if set, otherwise the first
// account the client's token can see. See Registry.FirstAccount.
func ResolveFirst(ctx context.Context, defaultID string, client *oanda.Client, log *slog.Logger) (*Account, error) {
	return shared.FirstAccount(ctx, defaultID, client, log)
}
