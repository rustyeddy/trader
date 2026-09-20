package risk

import (
	"context"
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/num"
)

// SizeInput is what a Sizer needs to size a candidate position.
//
// Not every field is required by every Sizer (ADR-061): fixedFraction
// Sizer's implementation of Sizer uses RiskFraction/StopDistance and
// ignores ReferencePrice; fullNotionalSizer's implementation uses
// ReferencePrice and ignores RiskFraction/StopDistance. checkSizeInput
// validates only what every Sizer needs (a constructed Account/
// Listing whose provider/currency agree); each concrete Sizer
// validates its own additional required fields itself and returns its
// own classifiable ErrInvalidSizeInput when one is missing — the same
// "small required core, each implementation validates what it
// actually consumes" split this codebase already uses for risk.Rule
// and strategy/smatrend's own ExitRule.
type SizeInput struct {
	// Account is the account the resulting Quantity would be sized
	// against — specifically its Equity(), BuyingPower(), and
	// Currency().
	Account account.Snapshot

	// Listing is the concrete, venue-specific instrument being sized.
	// Its Spec supplies the contract multiplier (per-unit cost/loss
	// scaling) and quantity increment (final rounding) a Sizer
	// applies.
	Listing instrument.Listing

	// RiskFraction is the fraction of Account.Equity() fixedFraction
	// Sizer may risk — for example 0.01 for 1%. Must be positive when
	// used; ignored by a Sizer that does not size from a risk
	// fraction.
	RiskFraction num.Rate

	// StopDistance is the adverse price distance fixedFractionSizer
	// sizes from: the per-unit price movement that would realize the
	// risk budget this call computes, not necessarily an
	// already-existing broker stop order. Must be positive when used;
	// ignored by a Sizer that does not size from a stop distance.
	StopDistance num.Price

	// ReferencePrice is the current price a price-based Sizer (for
	// example fullNotionalSizer) uses to convert available capital
	// directly into a quantity — the identical value pipeline.Input.
	// ReferencePrice already carries for risk.Input.ReferencePrice
	// (ADR-061), not a second, independently-supplied price that could
	// disagree with it. It names an honest sizing/valuation reference,
	// never a promise about the eventual broker fill price: a market
	// order's real fill can differ (slippage, a gap, a different fill
	// model) — see fullNotionalSizer's own doc comment. Must be
	// positive when used; ignored by a Sizer that does not size from a
	// reference price.
	ReferencePrice *num.Price
}

// Sizer computes a proposed order's Quantity from account
// equity/capital and whichever SizeInput fields its own concrete
// implementation consumes (ADR-006: sizing lives in risk).
type Sizer interface {
	Size(ctx context.Context, in SizeInput) (num.Quantity, error)
}

// checkSizeInput validates the fields every Sizer implementation
// needs regardless of its own sizing method, and returns nil or a
// wrapped ErrInvalidSizeInput describing the first problem found.
// RiskFraction/StopDistance/ReferencePrice are deliberately not
// checked here — see SizeInput's own doc comment for why each
// concrete Sizer validates its own required subset instead.
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
	if in.RiskFraction.Sign() <= 0 {
		return num.Quantity{}, fmt.Errorf("%w: risk fraction must be positive", ErrInvalidSizeInput)
	}
	if in.StopDistance.IsZero() {
		return num.Quantity{}, fmt.Errorf("%w: stop distance must be positive", ErrInvalidSizeInput)
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

// fullNotionalSizer is the exact (whole-share-quantized) 100%-invested
// Sizer issue #364/ADR-061 adds: it fully invests the account's
// available capital at ReferencePrice when sizing a new position,
// never a fraction of it and never a risk-budget-from-a-stop
// computation — the canonical "when Long, invest current available
// equity at the actual entry price" comparison mode issue #361/#362's
// own review asked for, replacing the RiskFraction=1/train-start-
// adverse-distance approximation research previously used.
//
// Available capital is min(Account.Equity(), Account.BuyingPower()),
// not Equity() alone (issue #364 review): Equity() is net liquidation
// value, which already includes any existing position's own value —
// sizing a *new* position from Equity() alone, ignoring BuyingPower(),
// could propose spending more than the broker actually has available.
// In a normal unlevered, flat-before-entry backtest (this Sizer's own
// motivating use case) the two are equal, so canonical "100% invested"
// research semantics are unaffected; the min() only matters when they
// are not.
//
// ReferencePrice, not the eventual fill price: this Sizer sizes
// against SizeInput.ReferencePrice exactly, and makes no claim about
// what a market order submitted from this Quantity will actually fill
// at — see ReferencePrice's own doc comment. A caller whose fill model
// can diverge from ReferencePrice (slippage, a gap) gets an exact
// notional at the *reference* price, not a guaranteed exact notional
// at the real fill. Canonical research claiming "exact 100% invested"
// must either use a fill model where a market order fills at the
// identical price ReferencePrice supplied (for example a next-bar-open
// reference price against a zero-slippage next-bar-open fill model),
// or report the result as reference-price-exact rather than
// fill-price-exact.
//
// Flat-only, at the whole-account level (issue #364/PR #371 review):
// order.IntentEnter means "open or increase" a position, but this
// Sizer always computes a brand-new order equal to the *entire*
// available budget. This Sizer's own contract is account-level 100%
// invested, not "100% of capital for one instrument alongside other
// existing exposure" — a materially different, narrower semantic this
// Sizer does not implement. Checking only SizeInput.Listing's own
// instrument for an existing position is not enough to protect that
// contract: with BuyingPower() >= Equity() (a real possibility on a
// leveraged/margin account; SizeInput places no upper bound on
// BuyingPower relative to Equity), a second full-equity-notional order
// against a *different* instrument would still be allowed through an
// instrument-scoped check, and even when BuyingPower() < Equity(),
// resulting portfolio exposure can still exceed the intended 100%
// depending on the existing position and margin model. Size therefore
// rejects outright with ErrFullNotionalRequiresFlat whenever Account
// holds *any* open position at all, regardless of instrument, rather
// than compute a target-exposure delta (a materially larger design
// this issue's own scope excludes — see ADR-061's own Alternatives
// Considered) — matching this Sizer's own motivating contract exactly:
// Flat -> invest up to 100% -> Long -> Flat.
type fullNotionalSizer struct{}

// NewFullNotionalSizer returns a Sizer implementing exact,
// whole-share-quantized full-notional position sizing. See
// fullNotionalSizer's own doc comment for its capital-budget and
// reference-price semantics.
func NewFullNotionalSizer() Sizer {
	return fullNotionalSizer{}
}

// Size implements Sizer.
func (fullNotionalSizer) Size(ctx context.Context, in SizeInput) (num.Quantity, error) {
	if err := ctx.Err(); err != nil {
		return num.Quantity{}, err
	}
	if err := checkSizeInput(in); err != nil {
		return num.Quantity{}, err
	}
	if in.ReferencePrice == nil || in.ReferencePrice.IsZero() {
		return num.Quantity{}, fmt.Errorf("%w: reference price must be positive for full-notional sizing", ErrInvalidSizeInput)
	}
	if positions := in.Account.Positions(); len(positions) != 0 {
		p := positions[0]
		return num.Quantity{}, fmt.Errorf("%w: account already holds a %s position in %s", ErrFullNotionalRequiresFlat, p.Side, p.Listing.InstrumentID())
	}

	equity := in.Account.Equity()
	buyingPower := in.Account.BuyingPower()
	cmp, err := buyingPower.Cmp(equity)
	if err != nil {
		return num.Quantity{}, fmt.Errorf("risk: comparing buying power to equity: %w", err)
	}
	budget := equity
	if cmp < 0 {
		budget = buyingPower
	}

	costPerUnit, err := in.ReferencePrice.MulRate(in.Listing.Spec().Multiplier())
	if err != nil {
		return num.Quantity{}, fmt.Errorf("risk: computing cost per unit: %w", err)
	}

	raw, err := budget.DivPrice(costPerUnit)
	if err != nil {
		return num.Quantity{}, fmt.Errorf("risk: computing raw full-notional quantity: %w", err)
	}

	increment := in.Listing.Spec().QuantityIncrement()
	rounded, err := raw.RoundDown(increment)
	if err != nil {
		return num.Quantity{}, fmt.Errorf("risk: rounding to quantity increment: %w", err)
	}

	// Identical post-rounding correction fixedFractionSizer applies —
	// see its own doc comment for why: DivPrice's own nearest-rounding
	// can push raw up across an increment boundary, so step back down
	// one increment at a time until the implied cost no longer exceeds
	// budget, never spending more than the account's own available
	// capital.
	for !rounded.IsZero() {
		impliedCost, err := costPerUnit.MulQuantity(rounded, budget.Currency())
		if err != nil {
			return num.Quantity{}, fmt.Errorf("risk: computing implied cost: %w", err)
		}
		cmp, err := impliedCost.Cmp(budget)
		if err != nil {
			return num.Quantity{}, fmt.Errorf("risk: comparing implied cost to budget: %w", err)
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
