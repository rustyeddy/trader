package risk

import (
	"context"
	"fmt"
	"strings"

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
	if !strings.EqualFold(in.Listing.Provider(), in.Account.Broker()) {
		return fmt.Errorf("%w: listing provider %q does not match account broker %q",
			ErrInvalidSizeInput, in.Listing.Provider(), in.Account.Broker())
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

	increment := in.Listing.Spec().QuantityIncrement()
	rounded, err := raw.RoundDown(increment)
	if err != nil {
		return num.Quantity{}, fmt.Errorf("risk: rounding to quantity increment: %w", err)
	}

	// raw itself is already rounded to nearest (RoundHalfEven) at
	// Quantity's own smallest representable step, inside DivPrice —
	// not the true, unrounded mathematical quotient. If that nearest-
	// rounding pushed raw up across an increment boundary, RoundDown
	// preserves the higher multiple, and the implied risk of the
	// resulting quantity can still exceed riskBudget even though the
	// quantity itself is a valid increment multiple (review feedback
	// on PR #196). Step back down by one increment at a time until the
	// implied risk no longer exceeds the budget. DivPrice's own
	// rounding error is bounded to at most half of Quantity's smallest
	// representable step, so in practice this never needs more than
	// one correction; the loop is written generally rather than
	// assuming that bound.
	for !rounded.IsZero() {
		impliedRisk, err := lossPerUnit.MulQuantity(rounded, riskBudget.Currency())
		if err != nil {
			return num.Quantity{}, fmt.Errorf("risk: computing implied risk: %w", err)
		}
		cmp, err := impliedRisk.Cmp(riskBudget)
		if err != nil {
			return num.Quantity{}, fmt.Errorf("risk: comparing implied risk to budget: %w", err)
		}
		if cmp <= 0 {
			break
		}
		next, err := rounded.Sub(increment)
		if err != nil {
			rounded = num.Quantity{}
			break
		}
		rounded = next
	}
	if rounded.IsZero() {
		return num.Quantity{}, ErrSizeRoundsToZero
	}

	return rounded, nil
}
