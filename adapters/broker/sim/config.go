package sim

import (
	"fmt"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// FillPriceSource supplies the price a market order.Request fills at.
// Broker never falls back to a wall clock or a global random source
// (ADR-015); an injected FillPriceSource is the sole authority for fill
// prices, matching how Clock and IDs are the sole authority for
// timestamps and identifiers. Implementations must be deterministic:
// the same listing, side, and call sequence must return the same price
// across separate runs so backtests stay reproducible. This package
// performs no bid/ask spread, slippage, or latency modeling of its own
// — side is passed through so an implementation can apply its own
// spread model if it chooses to.
type FillPriceSource interface {
	// Price returns the price a market order for listing/side fills
	// at. An error means no price is currently available for listing;
	// Submit reports it directly rather than guessing or falling back
	// to a stale value.
	Price(listing instrument.Listing, side order.Side) (num.Price, error)
}

// Deps supplies Broker's injected dependencies. Every field is
// required: Broker never falls back to a wall clock or a global random
// source (ADR-015), so a zero Deps is never usable.
type Deps struct {
	// Clock supplies every timestamp Broker produces — Event.Metadata
	// .Timestamp, Event.ObservedAt, and account.Snapshot.AsOf.
	Clock clock.Clock
	// IDs supplies every identifier Broker generates — Event.Metadata
	// .EventID and, for a market order's immediate fill, Fill.FillID.
	IDs *id.Generator
	// Prices supplies the fill price for every market order Submit
	// accepts (issue #149/M3-06). Limit and stop orders do not consult
	// it; they remain Working until issue #150 (M3-07) adds trigger
	// semantics.
	Prices FillPriceSource
}

func (d Deps) validate() error {
	if d.Clock == nil {
		return fmt.Errorf("%w: clock must be set", ErrInvalidConfig)
	}
	if d.IDs == nil {
		return fmt.Errorf("%w: id generator must be set", ErrInvalidConfig)
	}
	if d.Prices == nil {
		return fmt.Errorf("%w: fill price source must be set", ErrInvalidConfig)
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
