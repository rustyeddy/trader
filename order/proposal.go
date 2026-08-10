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
	if err := checkProposal(p); err != nil {
		return Proposal{}, fmt.Errorf("%w: %v", ErrInvalidProposal, err)
	}
	return p, nil
}

// checkProposal validates p's fields and returns a plain, unwrapped
// error describing the first problem found, or nil. It is shared by
// NewProposal and every later stage (NewRequest, NewOrder) that embeds a
// Proposal, so a Proposal built as a bare struct literal — bypassing
// NewProposal entirely — cannot slip an invalid value past a later
// stage's constructor.
func checkProposal(p Proposal) error {
	if p.Listing.InstrumentID().IsZero() {
		return fmt.Errorf("listing must be constructed")
	}
	if p.AccountID.IsZero() {
		return fmt.Errorf("account id must be set")
	}
	if !p.Side.valid() {
		return fmt.Errorf("invalid side %v", p.Side)
	}
	if !p.Type.valid() {
		return fmt.Errorf("invalid type %v", p.Type)
	}
	if !p.TimeInForce.valid() {
		return fmt.Errorf("invalid time in force %v", p.TimeInForce)
	}
	if err := validatePricePresence(p.Type, p.LimitPrice, p.StopPrice); err != nil {
		return err
	}
	if err := validatePriceAndQuantity(p.Listing, p.Quantity, p.LimitPrice, p.StopPrice); err != nil {
		return err
	}
	return nil
}
