package sim

import (
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account/margin"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/internal/id"
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

// AccountConfig describes one simulated account's identity,
// deterministic starting capital, and optional margin model.
// StartingCash's Currency becomes the account's home Currency.
type AccountConfig struct {
	AccountID    id.AccountID
	StartingCash num.Money

	// InitialMarginRatio configures the account's initial-margin model
	// (ADR-066): the minimum equity required per unit of gross position
	// notional — 1.0 is unlevered, 0.5 permits 2× gross exposure, 0.25
	// permits 4×. It must be positive when set.
	//
	// When nil, the account has no margin model: BuyingPower and
	// MarginAvailable mirror cash, MarginUsed is zero, and fills are
	// never refused for margin — the original M3 behavior. When set:
	//
	//   - Snapshot derives all three fields from the shared margin
	//     calculation (see accountState.marginFieldsLocked).
	//   - Every fill that increases a position is checked against the
	//     actual post-fill state, at the fill price and equity after its
	//     own commission (see accountState.checkFillMargin). An
	//     over-limit market order is returned StatusRejected with
	//     ReasonInsufficientMargin; an over-limit resting order is
	//     canceled by the broker with that reason in Order.CancelReason.
	//     Reduce-only and de-risking fills are never refused.
	//
	// Either way, MarginModelInfo describes the choice for a run
	// manifest.
	InitialMarginRatio *num.Rate
}

// marginModelName and marginModelVersion identify the initial-margin
// ratio model in MarginModelInfo.
const (
	marginModelName    = "initial-margin-ratio"
	marginModelVersion = "v1"
)

// MarginModelInfo identifies c's margin model for reproducibility
// records (ADR-028, ADR-066): Name "none" when InitialMarginRatio is
// nil, otherwise the ratio model with its ratio in Config, so two runs
// with different ratios are distinguishable.
func (c AccountConfig) MarginModelInfo() ModelInfo {
	if c.InitialMarginRatio == nil {
		return ModelInfo{Name: "none"}
	}
	return ModelInfo{Name: marginModelName, Version: marginModelVersion, Config: "ratio=" + c.InitialMarginRatio.String()}
}

func (c AccountConfig) validate() error {
	if c.AccountID.IsZero() {
		return fmt.Errorf("%w: account id must be set", ErrInvalidConfig)
	}
	if !c.StartingCash.IsValid() {
		return fmt.Errorf("%w: starting cash must be valid money", ErrInvalidConfig)
	}
	if _, err := c.marginPolicy(); err != nil {
		return err
	}
	return nil
}

// marginPolicy returns c's initial-margin policy, or nil when none is
// configured.
func (c AccountConfig) marginPolicy() (*margin.Ratio, error) {
	if c.InitialMarginRatio == nil {
		return nil, nil
	}
	r, err := margin.NewRatio(*c.InitialMarginRatio)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	return &r, nil
}
