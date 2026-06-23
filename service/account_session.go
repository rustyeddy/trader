package service

import (
	"context"
	"fmt"
)

// Account is a per-account session: every OANDA broker and account operation
// (summary, transactions, orders, trades, live runner, journal) hangs off an
// Account so a single Service can manage many accounts concurrently.
//
// Account holds no mutable state of its own in this phase; it carries the
// resolved account ID and a back-pointer to the owning Service for transport
// (the shared *oanda.Client and logger). Per-account mutable state (bots,
// trade→bot mapping) currently still lives on Service and is shared across
// accounts; it moves here when the multi-account serve daemon lands.
type Account struct {
	ID  string
	svc *Service
}

// Account returns the session for the given OANDA account ID, creating and
// caching it on first use. The ID is not validated against the token here;
// invalid IDs surface as errors from the first OANDA call. Use Accounts to
// enumerate the accounts the token can actually see.
func (s *Service) Account(ctx context.Context, id string) (*Account, error) {
	if id == "" {
		return nil, fmt.Errorf("account: empty account ID")
	}

	s.accountsMu.RLock()
	a, ok := s.accounts[id]
	s.accountsMu.RUnlock()
	if ok {
		return a, nil
	}

	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	// Re-check after acquiring the write lock — another goroutine may have
	// created it between the RUnlock and Lock above.
	if a, ok := s.accounts[id]; ok {
		return a, nil
	}
	if s.accounts == nil {
		s.accounts = make(map[string]*Account)
	}
	a = &Account{ID: id, svc: s}
	s.accounts[id] = a
	return a, nil
}

// Accounts returns a session for every account the token can access. The
// returned sessions are the same cached instances Account would return.
func (s *Service) Accounts(ctx context.Context) ([]*Account, error) {
	if s.OANDA == nil {
		return nil, fmt.Errorf("accounts: OANDA client not configured")
	}
	refs, err := s.OANDA.GetAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover accounts: %w", err)
	}
	out := make([]*Account, 0, len(refs))
	for _, r := range refs {
		a, err := s.Account(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// FirstAccount returns the account that read-only and UI operations default
// to: the configured default (s.AccountID) when set, otherwise the first
// account the token can see. Unlike DefaultAccount it never returns an
// AmbiguousAccountError — picking the first account is intentional for reads.
// It does NOT set s.AccountID, so DefaultAccount stays strict for mutations.
//
// Mutating operations must never resolve their account through FirstAccount;
// they require an explicitly named account.
func (s *Service) FirstAccount(ctx context.Context) (*Account, error) {
	if s.AccountID != "" {
		return s.Account(ctx, s.AccountID)
	}

	s.accountsMu.RLock()
	id := s.firstID
	s.accountsMu.RUnlock()
	if id != "" {
		return s.Account(ctx, id)
	}

	if s.OANDA == nil {
		return nil, fmt.Errorf("account: OANDA client not configured")
	}
	refs, err := s.OANDA.GetAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover accounts: %w", err)
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no accounts found for this token")
	}

	s.accountsMu.Lock()
	s.firstID = refs[0].ID
	s.accountsMu.Unlock()
	return s.Account(ctx, refs[0].ID)
}

// DefaultAccount resolves the Service's default account (s.AccountID,
// auto-discovered via ResolveAccount when unset) and returns its session.
// The back-compat Service-level broker methods delegate through this so
// existing single-account callers keep working unchanged. Presentation
// layers use it to back their legacy (un-scoped) routes.
func (s *Service) DefaultAccount(ctx context.Context) (*Account, error) {
	if err := s.ResolveAccount(ctx); err != nil {
		return nil, err
	}
	return s.Account(ctx, s.AccountID)
}
