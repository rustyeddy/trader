package account

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
)

// accountDetailsClient is the subset of the OANDA client that AccountSnapshot
// needs. *oanda.Client satisfies this interface.
type accountDetailsClient interface {
	GetAccountDetails(ctx context.Context, accountID string) (*oanda.AccountDetails, error)
	GetAccountChanges(ctx context.Context, accountID string, sinceID int64) (*oanda.AccountChangesResult, error)
}

// AccountSnapshot maintains a live, locally-cached view of an OANDA account.
// It seeds from GET /v3/accounts/{id} once at startup, then keeps itself
// current by polling GET /v3/accounts/{id}/changes on each interval.
//
// All read methods are safe for concurrent use from multiple goroutines.
type AccountSnapshot struct {
	client    accountDetailsClient
	accountID string
	log       *slog.Logger

	// mu guards the cached account state fields below.
	mu           sync.RWMutex
	balance      float64
	nav          float64
	unrealizedPL float64
	marginUsed   float64
	marginAvail  float64
	// openTrades is keyed by trade ID for O(1) add / remove / update.
	openTrades map[string]oanda.OpenTrade
	lastTxID   int64

	// startMu guards the started flag and the goroutine lifecycle.
	startMu sync.Mutex
	started bool
}

func newAccountSnapshot(client accountDetailsClient, accountID string, log *slog.Logger) *AccountSnapshot {
	if log == nil {
		log = slog.Default()
	}
	return &AccountSnapshot{
		client:     client,
		accountID:  accountID,
		log:        log,
		openTrades: make(map[string]oanda.OpenTrade),
	}
}

// Start fetches the initial full snapshot from OANDA, then launches a
// background goroutine that polls for incremental changes on each interval.
// Calling Start on an already-running snapshot is a no-op.
// The goroutine stops when ctx is cancelled.
func (s *AccountSnapshot) Start(ctx context.Context, interval time.Duration) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if s.started {
		return nil
	}

	details, err := s.client.GetAccountDetails(ctx, s.accountID)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.balance = details.Balance
	s.nav = details.NAV
	s.unrealizedPL = details.UnrealizedPL
	s.marginUsed = details.MarginUsed
	s.marginAvail = details.MarginAvail
	s.openTrades = make(map[string]oanda.OpenTrade, len(details.OpenTrades))
	for _, t := range details.OpenTrades {
		s.openTrades[t.ID] = t
	}
	s.lastTxID = details.LastTransactionID
	s.mu.Unlock()

	s.started = true
	go s.pollLoop(ctx, interval)
	return nil
}

// IsRunning reports whether the background poll goroutine is active.
func (s *AccountSnapshot) IsRunning() bool {
	s.startMu.Lock()
	r := s.started
	s.startMu.Unlock()
	return r
}

func (s *AccountSnapshot) pollLoop(ctx context.Context, interval time.Duration) {
	defer func() {
		s.startMu.Lock()
		s.started = false
		s.startMu.Unlock()
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.RLock()
			sinceID := s.lastTxID
			s.mu.RUnlock()

			changes, err := s.client.GetAccountChanges(ctx, s.accountID, sinceID)
			if err != nil {
				s.log.Warn("account snapshot: poll failed", "account", s.accountID, "err", err)
				continue
			}
			s.applyChanges(changes)
		}
	}
}

// applyChanges updates the cached snapshot from a changes poll response.
// Structural changes (opened / closed / reduced trades) are applied first,
// then price-dependent state fields are overwritten.
func (s *AccountSnapshot) applyChanges(c *oanda.AccountChangesResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Add newly opened trades.
	for _, t := range c.TradesOpened {
		s.openTrades[t.ID] = t
	}
	// Remove fully-closed trades.
	for _, id := range c.TradesClosed {
		delete(s.openTrades, id)
	}
	// Update units for partially-closed trades.
	for id, newUnits := range c.TradesReduced {
		if t, ok := s.openTrades[id]; ok {
			t.Units = newUnits
			s.openTrades[id] = t
		}
	}
	// Overwrite price-dependent state.
	s.nav = c.NAV
	s.unrealizedPL = c.UnrealizedPL
	s.marginUsed = c.MarginUsed
	s.marginAvail = c.MarginAvail
	// Update per-trade unrealized PL.
	for id, upl := range c.TradeState {
		if t, ok := s.openTrades[id]; ok {
			t.UnrealizedPL = upl
			s.openTrades[id] = t
		}
	}
	// Update balance if a fill occurred.
	if c.BalanceAfterFill > 0 {
		s.balance = c.BalanceAfterFill
	}
	if c.LastTransactionID > s.lastTxID {
		s.lastTxID = c.LastTransactionID
	}
}

// ─── read accessors ───────────────────────────────────────────────────────────

// NAV returns the current net asset value (equity) from the latest poll.
func (s *AccountSnapshot) NAV() float64 {
	s.mu.RLock()
	v := s.nav
	s.mu.RUnlock()
	return v
}

// Balance returns the realized account balance (updated after fills).
func (s *AccountSnapshot) Balance() float64 {
	s.mu.RLock()
	v := s.balance
	s.mu.RUnlock()
	return v
}

// OpenTrades returns a snapshot of the current open trades as a slice.
// The caller receives its own copy; mutations do not affect the cache.
func (s *AccountSnapshot) OpenTrades() []oanda.OpenTrade {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]oanda.OpenTrade, 0, len(s.openTrades))
	for _, t := range s.openTrades {
		out = append(out, t)
	}
	return out
}

// Summary returns the current account state as an AccountSummary, suitable
// for REST/MCP responses that previously called GetAccountSummary directly.
func (s *AccountSnapshot) Summary() *oanda.AccountSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &oanda.AccountSummary{
		ID:           s.accountID,
		Balance:      s.balance,
		NAV:          s.nav,
		UnrealizedPL: s.unrealizedPL,
		MarginUsed:   s.marginUsed,
		MarginAvail:  s.marginAvail,
	}
}
