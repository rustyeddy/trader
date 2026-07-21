package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rustyeddy/trader/brokers"
)

// Account is a per-account session: every OANDA broker and account operation
// (summary, transactions, orders, trades, live runner, journal) hangs off an
// Account so a single Service can manage many accounts concurrently.
type Account struct {
	ID  string
	svc *Service

	// snapMu guards snapshot; only one snapshot per account is ever created.
	snapMu   sync.RWMutex
	snapshot *AccountSnapshot
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

// EnsureSnapshot starts the account's background changes-poll goroutine if it
// is not already running. The goroutine binds to ctx, so it stops when ctx is
// cancelled. Safe to call from multiple goroutines; only one goroutine is ever
// started per Account.
func (a *Account) EnsureSnapshot(ctx context.Context, interval time.Duration) {
	a.snapMu.Lock()
	if a.snapshot == nil {
		a.snapshot = newAccountSnapshot(a.broker(), a.ID, a.svc.Log)
	}
	snap := a.snapshot
	a.snapMu.Unlock()

	if !snap.IsRunning() {
		if err := snap.Start(ctx, interval); err != nil {
			if a.svc.Log != nil {
				a.svc.Log.Warn("account snapshot: start failed", "err", err, "account", a.ID)
			}
		}
	}
}

// getSnapshot returns the running AccountSnapshot for this account, or nil if
// no snapshot is running. Callers fall back to direct OANDA calls when nil.
func (a *Account) getSnapshot() *AccountSnapshot {
	a.snapMu.RLock()
	s := a.snapshot
	a.snapMu.RUnlock()
	if s != nil && s.IsRunning() {
		return s
	}
	return nil
}

// broker narrows a.svc.OANDA to the brokers.Broker execution interface.
// *oanda.Client satisfies Broker unchanged (see brokers/broker.go) — this
// is the first live call site to depend on the interface instead of the
// concrete client (docs/Plans/service-refactor.org, phase 2). Only
// execution methods (order placement, close, stop, account summary) go
// through here; pricing/candle calls stay on a.svc.OANDA directly, since
// those are DataProvider-shaped and out of scope for this phase.
func (a *Account) broker() brokers.Broker {
	return a.svc.OANDA
}
