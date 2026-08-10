package order

import (
	"fmt"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// Proposal is a concrete order candidate before risk approval: what
// execution planning wants to submit, not yet validated against risk or
// assigned a Trader order identity. See Request for the next stage.
type Proposal struct {
	// Listing is the venue-specific tradable instrument this proposal
	// targets.
	Listing instrument.Listing
	// AccountID identifies the Trader-managed account this proposal
	// would trade against.
	AccountID id.AccountID
	Side      Side
	Type      Type
	// TimeInForce controls how long the resulting order would remain
	// eligible for execution.
	TimeInForce TimeInForce
	// Quantity is the requested order size. It must be greater than
	// zero and a multiple of Listing's quantity increment.
	Quantity num.Quantity
	// LimitPrice is required for Limit and StopLimit, and must be nil
	// otherwise.
	LimitPrice *num.Price
	// StopPrice is required for Stop and StopLimit, and must be nil
	// otherwise.
	StopPrice *num.Price
	// ReduceOnly, when true, means this proposal may only reduce an
	// existing position, never open or increase one.
	ReduceOnly bool
	// Metadata carries this proposal's correlation and causation
	// context.
	Metadata id.Metadata
}

// NewProposal validates and returns a Proposal. Listing must be a
// constructed Listing; AccountID must be non-zero; Side, Type, and
// TimeInForce must be one of their defined values; Quantity must be
// positive and a multiple of Listing's quantity increment; LimitPrice
// and StopPrice must be present or nil exactly as Type requires, and
// when present must be exact multiples of Listing's tick size.
func NewProposal(p Proposal) (Proposal, error) {
	if p.Listing.InstrumentID().IsZero() {
		return Proposal{}, fmt.Errorf("%w: listing must be constructed", ErrInvalidProposal)
	}
	if p.AccountID.IsZero() {
		return Proposal{}, fmt.Errorf("%w: account id must be set", ErrInvalidProposal)
	}
	if !p.Side.valid() {
		return Proposal{}, fmt.Errorf("%w: invalid side %v", ErrInvalidProposal, p.Side)
	}
	if !p.Type.valid() {
		return Proposal{}, fmt.Errorf("%w: invalid type %v", ErrInvalidProposal, p.Type)
	}
	if !p.TimeInForce.valid() {
		return Proposal{}, fmt.Errorf("%w: invalid time in force %v", ErrInvalidProposal, p.TimeInForce)
	}
	if err := validatePricePresence(p.Type, p.LimitPrice, p.StopPrice); err != nil {
		return Proposal{}, fmt.Errorf("%w: %v", ErrInvalidProposal, err)
	}
	if err := validatePriceAndQuantity(p.Listing, p.Quantity, p.LimitPrice, p.StopPrice); err != nil {
		return Proposal{}, fmt.Errorf("%w: %v", ErrInvalidProposal, err)
	}
	return p, nil
}
