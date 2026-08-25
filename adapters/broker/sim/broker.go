package sim

import (
	"context"
	"fmt"
	"sync"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"
)

// Broker is a deterministic, in-memory implementation of broker.Broker
// (see the package doc comment). Construct one with NewBroker; the zero
// Broker is not usable.
//
// # Concurrency ownership
//
// Broker's own mutex protects only its account map and closed flag —
// Accounts, OpenAccount, and Close never block on any account's own
// state. Each account's mutable state is independently guarded by its
// own accountState.mu (see account.go), so operations against two
// different accounts never contend with each other. Broker starts no
// goroutines of its own.
type Broker struct {
	name string
	deps Deps

	mu       sync.RWMutex
	closed   bool
	accounts map[id.AccountID]*accountState
}

var _ brokerpkg.Broker = (*Broker)(nil)

// NewBroker returns a Broker named name, using deps for every timestamp
// and identifier it produces, with one account per entry in configs.
// name must be non-empty, deps must have both fields set, every config
// must validate, and no two configs may share an AccountID. Each
// account's starting account.Snapshot is built immediately, timestamped
// with deps.Clock.Now() at construction — the same deps and configs
// reproduce byte-identical starting state across separate runs.
func NewBroker(name string, deps Deps, configs ...AccountConfig) (*Broker, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: name must be set", ErrInvalidConfig)
	}
	if err := deps.validate(); err != nil {
		return nil, err
	}

	accounts := make(map[id.AccountID]*accountState, len(configs))
	now := deps.Clock.Now()
	for i, cfg := range configs {
		if err := cfg.validate(); err != nil {
			return nil, fmt.Errorf("account config %d: %w", i, err)
		}
		if _, exists := accounts[cfg.AccountID]; exists {
			return nil, fmt.Errorf("%w: account config %d: duplicate account id %s", ErrInvalidConfig, i, cfg.AccountID)
		}

		ref, err := account.NewReference(account.Reference{AccountID: cfg.AccountID, Broker: name})
		if err != nil {
			return nil, fmt.Errorf("account config %d: %w", i, err)
		}

		zero, err := zeroMoney(cfg.StartingCash.Currency())
		if err != nil {
			return nil, fmt.Errorf("account config %d: %w", i, err)
		}

		accounts[cfg.AccountID] = &accountState{
			ref:       ref,
			currency:  cfg.StartingCash.Currency(),
			cash:      cfg.StartingCash,
			zero:      zero,
			asOf:      now,
			orders:    make(map[id.OrderID]order.Order),
			positions: make(map[positionKey]order.Position),
			changed:   make(chan struct{}),
		}
	}

	return &Broker{name: name, deps: deps, accounts: accounts}, nil
}

// Name implements broker.Broker.
func (b *Broker) Name() string { return b.name }

// Accounts implements broker.Broker.
func (b *Broker) Accounts(ctx context.Context) ([]account.Reference, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return nil, brokerpkg.ErrClosed
	}

	refs := make([]account.Reference, 0, len(b.accounts))
	for _, state := range b.accounts {
		refs = append(refs, state.ref)
	}
	return refs, nil
}

// OpenAccount implements broker.Broker.
func (b *Broker) OpenAccount(ctx context.Context, accountID id.AccountID) (brokerpkg.Account, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return nil, brokerpkg.ErrClosed
	}

	state, ok := b.accounts[accountID]
	if !ok {
		return nil, brokerpkg.ErrAccountNotFound
	}
	return &accountHandle{broker: b, state: state}, nil
}

// Close implements broker.Broker. It marks every account handle opened
// from this Broker as closed too, and wakes every EventReader currently
// blocked in Next so each observes the close and returns io.EOF (see
// accountState's doc comment and eventReader.Next) rather than blocking
// forever. Close itself never returns an error and is safe to call more
// than once.
func (b *Broker) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	accounts := make([]*accountState, 0, len(b.accounts))
	for _, state := range b.accounts {
		accounts = append(accounts, state)
	}
	b.mu.Unlock()

	for _, state := range accounts {
		state.mu.Lock()
		if !state.closed {
			state.closed = true
			close(state.changed)
		}
		state.mu.Unlock()
	}
	return nil
}

// isClosed reports whether Close has already been called.
func (b *Broker) isClosed() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.closed
}
