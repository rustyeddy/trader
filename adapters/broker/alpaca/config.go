package alpaca

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
)

// Deps supplies Broker's injected dependencies, mirroring
// adapters/broker/sim.Deps's shape: Broker never falls back to a wall
// clock or a global random source, so a zero Deps is never usable.
type Deps struct {
	// Clock supplies every timestamp this adapter assigns itself —
	// Event.ObservedAt/Metadata.Timestamp for a synthesized event, and
	// the polling EventReader's own baseline. It does not supply order
	// or fill timestamps Alpaca itself reports (those come from the
	// wire response).
	Clock clock.Clock
	// IDs supplies every identifier this adapter generates —
	// Event.Metadata.EventID for a synthesized event. Trader's own
	// FillID is likewise generated here for a synthesized fill (see
	// event_reader.go); it never comes from Alpaca, which reports no
	// separate execution id at the resolution this adapter's polling
	// (rather than a real fills/executions feed) works at.
	IDs *id.Generator
	// Resolver resolves an Alpaca wire symbol (for example "AAPL") back
	// to an instrument.Listing, and the reverse via req.Listing.Symbol()
	// when building a wire request. See accountHandle's own doc comment
	// for why this is the same "alpaca" provider tag the equity Market
	// Data provider (issue #297) registers Listings under.
	Resolver instrument.Resolver
	// PollInterval controls how often the EventReader polls Alpaca's
	// order list when it has nothing buffered to deliver. Non-positive
	// selects a package default (2s) — Alpaca's paper API rate limit is
	// generous enough for this cadence.
	PollInterval time.Duration
}

const defaultPollInterval = 2 * time.Second

func (d Deps) validate() error {
	if d.Clock == nil {
		return fmt.Errorf("%w: clock must be set", ErrInvalidConfig)
	}
	if d.IDs == nil {
		return fmt.Errorf("%w: id generator must be set", ErrInvalidConfig)
	}
	if d.Resolver == nil {
		return fmt.Errorf("%w: resolver must be set", ErrInvalidConfig)
	}
	return nil
}

func (d Deps) pollInterval() time.Duration {
	if d.PollInterval <= 0 {
		return defaultPollInterval
	}
	return d.PollInterval
}

// AccountConfig identifies the single Trader-managed account this
// Broker exposes. Unlike adapters/broker/sim, which can hold many
// simulated accounts, exactly one Alpaca key/secret pair authenticates
// as exactly one Alpaca paper account — there is no multi-account
// discovery to perform, so Broker.Accounts always reports this one
// configured Reference and Broker.OpenAccount accepts only this one
// AccountID.
type AccountConfig struct {
	// AccountID is Trader's own identifier for this account — an
	// operator-assigned identity, not anything Alpaca reports.
	AccountID id.AccountID
}

func (c AccountConfig) validate() error {
	if c.AccountID.IsZero() {
		return fmt.Errorf("%w: account id must be set", ErrInvalidConfig)
	}
	return nil
}
