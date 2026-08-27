package risk

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// SizeInput is what a Sizer needs to size a candidate position.
type SizeInput struct {
	// Account is the account the resulting Quantity would be sized
	// against — specifically its Equity() and Currency().
	Account account.Snapshot

	// Listing is the concrete, venue-specific instrument being sized.
	// Its Spec supplies the contract multiplier (loss-per-unit scaling)
	// and quantity increment (final rounding) a Sizer applies.
	Listing instrument.Listing

	// RiskFraction is the fraction of Account.Equity() this size may
	// risk — for example 0.01 for 1%. Must be positive.
	RiskFraction num.Rate

	// StopDistance is the adverse price distance used for sizing: the
	// per-unit price movement that would realize the risk budget this
	// call computes, not necessarily an already-existing broker stop
	// order. Must be positive.
	StopDistance num.Price
}

// Sizer computes a proposed order's Quantity from account
// equity/capital, a configured per-trade risk fraction, and a defined
// adverse-price/stop distance (ADR-006: sizing lives in risk).
type Sizer interface {
	Size(ctx context.Context, in SizeInput) (num.Quantity, error)
}

// checkSizeInput validates in's fields and returns nil, or a wrapped
// ErrInvalidSizeInput describing the first problem found.
func checkSizeInput(in SizeInput) error {
	if in.Account.AccountID().IsZero() {
		return fmt.Errorf("%w: account must be constructed", ErrInvalidSizeInput)
	}
	if in.Listing.InstrumentID().IsZero() {
		return fmt.Errorf("%w: listing must be constructed", ErrInvalidSizeInput)
	}
	if in.RiskFraction.Sign() <= 0 {
		return fmt.Errorf("%w: risk fraction must be positive", ErrInvalidSizeInput)
	}
	if in.StopDistance.IsZero() {
		return fmt.Errorf("%w: stop distance must be positive", ErrInvalidSizeInput)
	}
	if !in.Account.Equity().Currency().Equal(in.Listing.Spec().SettlementCurrency()) {
		return fmt.Errorf("%w: account equity currency %s does not match listing settlement currency %s (ADR-030: no implicit conversion)",
			ErrInvalidSizeInput, in.Account.Equity().Currency(), in.Listing.Spec().SettlementCurrency())
	}
	return nil
}

// fixedFractionSizer is the v0 reference Sizer implementation (issue
// #181, M4-06): it risks exactly RiskFraction of account equity per
// call, deriving a Quantity from StopDistance and the listing's own
// contract multiplier, then rounding down to the listing's quantity
// increment so rounding can never cause the result to exceed the
// configured risk budget (ADR-030).
type fixedFractionSizer struct{}

// NewFixedFractionSizer returns a Sizer implementing fixed-fraction
// account-risk position sizing.
func NewFixedFractionSizer() Sizer {
	return fixedFractionSizer{}
}

// Size implements Sizer.
func (fixedFractionSizer) Size(ctx context.Context, in SizeInput) (num.Quantity, error) {
	if err := ctx.Err(); err != nil {
		return num.Quantity{}, err
	}
	if err := checkSizeInput(in); err != nil {
		return num.Quantity{}, err
	}

	riskBudget, err := in.Account.Equity().MulRate(in.RiskFraction)
	if err != nil {
		return num.Quantity{}, fmt.Errorf("risk: computing risk budget: %w", err)
	}

	lossPerUnit, err := in.StopDistance.MulRate(in.Listing.Spec().Multiplier())
	if err != nil {
		return num.Quantity{}, fmt.Errorf("risk: computing loss per unit: %w", err)
	}

	raw, err := riskBudget.DivPrice(lossPerUnit)
	if err != nil {
		return num.Quantity{}, fmt.Errorf("risk: computing raw quantity: %w", err)
	}

	rounded, err := raw.RoundDown(in.Listing.Spec().QuantityIncrement())
	if err != nil {
		return num.Quantity{}, fmt.Errorf("risk: rounding to quantity increment: %w", err)
	}
	if rounded.IsZero() {
		return num.Quantity{}, ErrSizeRoundsToZero
	}

	return rounded, nil
}
