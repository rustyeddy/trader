package sim

import (
	"fmt"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
)

// Deps supplies Broker's injected dependencies. Both fields are
// required: Broker never falls back to a wall clock or a global random
// source (ADR-015), so a zero Deps is never usable.
type Deps struct {
	// Clock supplies every timestamp Broker produces — Event.Metadata
	// .Timestamp, Event.ObservedAt, and account.Snapshot.AsOf.
	Clock clock.Clock
	// IDs supplies every identifier Broker generates — currently,
	// Event.Metadata.EventID.
	IDs *id.Generator
}

func (d Deps) validate() error {
	if d.Clock == nil {
		return fmt.Errorf("%w: clock must be set", ErrInvalidConfig)
	}
	if d.IDs == nil {
		return fmt.Errorf("%w: id generator must be set", ErrInvalidConfig)
	}
	return nil
}

// AccountConfig describes one simulated account's identity and
// deterministic starting capital. StartingCash's Currency becomes the
// account's home Currency; equity, buying power, and margin available
// all start equal to StartingCash, with margin used and PnL starting at
// zero — this package models no leverage or margin policy of its own
// (that is risk's concern, M4).
type AccountConfig struct {
	AccountID    id.AccountID
	StartingCash num.Money
}

func (c AccountConfig) validate() error {
	if c.AccountID.IsZero() {
		return fmt.Errorf("%w: account id must be set", ErrInvalidConfig)
	}
	if !c.StartingCash.IsValid() {
		return fmt.Errorf("%w: starting cash must be valid money", ErrInvalidConfig)
	}
	return nil
}
