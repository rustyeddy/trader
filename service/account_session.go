package service

import (
	"context"

	"github.com/rustyeddy/trader/account"
)

// Account returns the session for the given OANDA account ID, creating and
// caching it on first use. The ID is not validated against the token here;
// invalid IDs surface as errors from the first OANDA call. Use Accounts to
// enumerate the accounts the token can actually see.
func (s *Service) Account(ctx context.Context, id string) (*account.Account, error) {
	return s.registry.Account(ctx, id, s.OANDA, s.Log)
}

// Accounts returns a session for every account the token can access. The
// returned sessions are the same cached instances Account would return.
func (s *Service) Accounts(ctx context.Context) ([]*account.Account, error) {
	return s.registry.Accounts(ctx, s.OANDA, s.Log)
}

// FirstAccount returns the account that read-only and UI operations default
// to: the configured default (s.AccountID) when set, otherwise the first
// account the token can see. Unlike DefaultAccount it never returns an
// AmbiguousAccountError — picking the first account is intentional for reads.
// It does NOT set s.AccountID, so DefaultAccount stays strict for mutations.
//
// Mutating operations must never resolve their account through FirstAccount;
// they require an explicitly named account.
func (s *Service) FirstAccount(ctx context.Context) (*account.Account, error) {
	return s.registry.FirstAccount(ctx, s.AccountID, s.OANDA, s.Log)
}

// DefaultAccount resolves the Service's default account (s.AccountID,
// auto-discovered via ResolveAccount when unset) and returns its session.
// The back-compat Service-level broker methods delegate through this so
// existing single-account callers keep working unchanged. Presentation
// layers use it to back their legacy (un-scoped) routes.
func (s *Service) DefaultAccount(ctx context.Context) (*account.Account, error) {
	if err := s.ResolveAccount(ctx); err != nil {
		return nil, err
	}
	return s.registry.Account(ctx, s.AccountID, s.OANDA, s.Log)
}
