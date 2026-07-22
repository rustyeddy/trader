package account

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/rustyeddy/trader/brokers/oanda"
)

// Registry caches live-session Accounts keyed by OANDA account ID. The zero
// value is ready to use — no constructor is required, so a Registry can sit
// as a plain value field on any struct (including one built via a bare
// struct literal in tests) without an explicit initialization step.
//
// Registry does not hold an OANDA client or logger of its own; each method
// takes them as parameters, supplied fresh by the caller on every call.
// This mirrors the pre-move behavior more faithfully than caching them at
// construction time would: a cached session's OANDA client/logger were
// previously read live through a *Service back-reference on every call, not
// frozen once.
type Registry struct {
	mu       sync.RWMutex
	accounts map[string]*Account
	firstID  string
}

// Account returns the session for the given OANDA account ID, creating and
// caching it on first use. The ID is not validated against the token here;
// invalid IDs surface as errors from the first OANDA call. Use Accounts to
// enumerate the accounts the token can actually see.
func (r *Registry) Account(ctx context.Context, id string, oandaClient *oanda.Client, log *slog.Logger) (*Account, error) {
	if id == "" {
		return nil, fmt.Errorf("account: empty account ID")
	}

	r.mu.RLock()
	a, ok := r.accounts[id]
	r.mu.RUnlock()
	if ok {
		return a, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Re-check after acquiring the write lock — another goroutine may have
	// created it between the RUnlock and Lock above.
	if a, ok := r.accounts[id]; ok {
		return a, nil
	}
	if r.accounts == nil {
		r.accounts = make(map[string]*Account)
	}
	a = NewSession(id, oandaClient, log)
	r.accounts[id] = a
	return a, nil
}

// Accounts returns a session for every account the token can access. The
// returned sessions are the same cached instances Account would return.
func (r *Registry) Accounts(ctx context.Context, oandaClient *oanda.Client, log *slog.Logger) ([]*Account, error) {
	if oandaClient == nil {
		return nil, fmt.Errorf("accounts: OANDA client not configured")
	}
	refs, err := oandaClient.GetAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover accounts: %w", err)
	}
	out := make([]*Account, 0, len(refs))
	for _, ref := range refs {
		a, err := r.Account(ctx, ref.ID, oandaClient, log)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// FirstAccount returns the account that read-only and UI operations default
// to: defaultID (the caller's configured default) when set, otherwise the
// first account the token can see. Unlike a strict resolve, it never errors
// on ambiguity — picking the first account is intentional for reads. It
// does not mutate the caller's configured default; callers needing strict
// resolution for mutations should resolve their default explicitly first.
func (r *Registry) FirstAccount(ctx context.Context, defaultID string, oandaClient *oanda.Client, log *slog.Logger) (*Account, error) {
	if defaultID != "" {
		return r.Account(ctx, defaultID, oandaClient, log)
	}

	r.mu.RLock()
	id := r.firstID
	r.mu.RUnlock()
	if id != "" {
		return r.Account(ctx, id, oandaClient, log)
	}

	if oandaClient == nil {
		return nil, fmt.Errorf("account: OANDA client not configured")
	}
	refs, err := oandaClient.GetAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover accounts: %w", err)
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no accounts found for this token")
	}

	r.mu.Lock()
	r.firstID = refs[0].ID
	r.mu.Unlock()
	return r.Account(ctx, refs[0].ID, oandaClient, log)
}
