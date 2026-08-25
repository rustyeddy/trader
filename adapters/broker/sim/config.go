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
	// Info identifies this configured model instance (issue #153,
	// M3-10), the same reproducibility surface SlippageModel and
	// CommissionModel expose.
	Info() ModelInfo
}

// IntrabarPolicy selects how Broker.Advance resolves an Observation
// that would trigger more than one of an account's pending orders for
// the same listing within one bar — OHLC data alone cannot establish
// which order's trigger the market actually reached first (ADR-026).
type IntrabarPolicy uint8

const (
	// IntrabarRejectAmbiguous is IntrabarPolicy's zero value and Deps's
	// default: Advance reports ErrAmbiguousIntrabarOrder and leaves
	// every one of the conflicting account's orders for that listing
	// untouched, rather than guessing which triggered first.
	IntrabarRejectAmbiguous IntrabarPolicy = iota
	// IntrabarPessimistic is declared but not implemented (ADR-026):
	// Advance reports broker.ErrUnsupported if selected. No scenario
	// in issue #150's scope forces a specific resolution algorithm;
	// this value exists so a later issue can implement one without a
	// further public API change.
	IntrabarPessimistic
)

// Deps supplies Broker's injected dependencies. Clock, IDs, and Prices
// are required: Broker never falls back to a wall clock or a global
// random source (ADR-015), so a zero Deps is never usable.
// IntrabarPolicy is not required — its zero value,
// IntrabarRejectAmbiguous, is itself Deps's deliberate, safe default.
type Deps struct {
	// Clock supplies every timestamp Broker produces — Event.Metadata
	// .Timestamp, Event.ObservedAt, and account.Snapshot.AsOf. Advance
	// (issue #150/M3-07) also derives every timestamp it produces from
	// Clock, not from an Observation's own Time; a caller driving a
	// backtest is expected to keep Clock synchronized with each
	// Observation it advances (ADR-026).
	Clock clock.Clock
	// IDs supplies every identifier Broker generates — Event.Metadata
	// .EventID and, for a market order's immediate fill, Fill.FillID.
	IDs *id.Generator
	// Prices supplies the fill price for every market order Submit
	// accepts (issue #149/M3-06). Limit and stop orders do not consult
	// it; Broker.Advance fills them instead, at a price derived from
	// each Observation (issue #150/M3-07, ADR-026).
	Prices FillPriceSource
	// IntrabarPolicy selects how Broker.Advance resolves an ambiguous
	// Observation (ADR-026). The zero value, IntrabarRejectAmbiguous,
	// is a legitimate, safe default and requires no explicit setting.
	IntrabarPolicy IntrabarPolicy
	// Slippage adjusts a Market/Stop fill's base price (issue #153,
	// M3-10). Nil (the default) means no slippage — the exact base
	// price from Prices or the observation trigger/gap rules is used
	// unchanged. Never consulted for Limit fills.
	Slippage SlippageModel
	// Commission computes the commission owed for a fill (issue #153,
	// M3-10). Nil (the default) means no commission — this package
	// invents no fee model of its own.
	Commission CommissionModel
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
