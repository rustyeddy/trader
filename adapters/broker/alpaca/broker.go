package alpaca

import (
	"context"
	"fmt"
	"sync"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
)

// Broker is broker.Broker's implementation against Alpaca's paper
// Trading API (issue #301, EQ-08). Construct one with NewBroker; the
// zero Broker is not usable.
//
// Unlike adapters/broker/sim, which can hold many independent simulated
// accounts, exactly one Alpaca key/secret pair authenticates as exactly
// one Alpaca account — Broker therefore always exposes exactly the one
// account.Reference named by its AccountConfig, and OpenAccount accepts
// only that one id.AccountID.
type Broker struct {
	name   string
	client *Client
	deps   Deps
	ref    account.Reference
	corr   *correlator

	mu     sync.Mutex
	closed bool
}

var _ brokerpkg.Broker = (*Broker)(nil)

// NewBroker returns a Broker named name (typically "alpaca"), using
// client for every Alpaca API call and deps for every timestamp,
// identifier, and listing resolution this adapter performs itself.
// name must be non-empty, client must be non-nil, deps must validate,
// and cfg must validate.
func NewBroker(name string, client *Client, deps Deps, cfg AccountConfig) (*Broker, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: name must be set", ErrInvalidConfig)
	}
	if client == nil {
		return nil, fmt.Errorf("%w: client must be set", ErrInvalidConfig)
	}
	if err := deps.validate(); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	ref, err := account.NewReference(account.Reference{AccountID: cfg.AccountID, Broker: name})
	if err != nil {
		return nil, err
	}
	return &Broker{
		name:   name,
		client: client,
		deps:   deps,
		ref:    ref,
		corr:   newCorrelator(),
	}, nil
}

// Name implements broker.Broker.
func (b *Broker) Name() string { return b.name }

// Accounts implements broker.Broker. It always reports exactly the one
// account.Reference this Broker was configured with — there is no
// remote discovery call, since one Alpaca credential pair authenticates
// as exactly one account.
func (b *Broker) Accounts(ctx context.Context) ([]account.Reference, error) {
	if b.isClosed() {
		return nil, brokerpkg.ErrClosed
	}
	return []account.Reference{b.ref}, nil
}

// OpenAccount implements broker.Broker.
func (b *Broker) OpenAccount(ctx context.Context, accountID id.AccountID) (brokerpkg.Account, error) {
	if b.isClosed() {
		return nil, brokerpkg.ErrClosed
	}
	if accountID != b.ref.AccountID {
		return nil, brokerpkg.ErrAccountNotFound
	}
	return &accountHandle{broker: b}, nil
}

// Close implements broker.Broker. It has no persistent connection to
// release (this adapter is REST-only in Phase 1, see the package doc
// comment) beyond marking itself closed and waking every EventReader
// currently blocked in Next, so each observes the close and returns
// io.EOF rather than blocking forever — the same contract
// adapters/broker/sim.Broker.Close honors. Safe to call more than once.
func (b *Broker) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.mu.Unlock()
	b.corr.close()
	return nil
}

func (b *Broker) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}
