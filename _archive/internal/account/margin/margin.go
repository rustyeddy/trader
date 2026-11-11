package margin

import (
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/num"
)

// Valued is a position's size in one listing, valued at one price.
type Valued struct {
	// Listing supplies the contract multiplier and settlement currency.
	Listing instrument.Listing
	// Quantity is the position's magnitude. Direction does not matter
	// for gross exposure, so there is no side.
	Quantity num.Quantity
	// Price is the single valuation price for the whole Quantity.
	Price num.Price
}

// Notional returns |Quantity| × Price × multiplier in currency. It
// reports ErrCurrencyMismatch if v's listing does not settle in
// currency.
func Notional(v Valued, currency num.Currency) (num.Money, error) {
	if err := checkListing(v.Listing, currency); err != nil {
		return num.Money{}, err
	}
	perUnit, err := v.Price.MulRate(v.Listing.Spec().Multiplier())
	if err != nil {
		return num.Money{}, fmt.Errorf("margin: value per unit of %s: %w", v.Listing.InstrumentID(), err)
	}
	notional, err := perUnit.MulQuantity(v.Quantity, currency)
	if err != nil {
		return num.Money{}, fmt.Errorf("margin: notional of %s: %w", v.Listing.InstrumentID(), err)
	}
	return notional, nil
}

// Policy computes the initial margin one valued position requires.
//
// It is the per-position extension point ADR-066 reserves for
// listing- or instrument-specific margin; v1's only implementation is
// Ratio.
//
// Validate reports whether the policy can compute requirements at all.
// Account and Assess call it once, up front, so an invalid policy is
// rejected even when no position ever reaches RequiredMargin (a flat
// account, or a closing change).
type Policy interface {
	Validate() error
	RequiredMargin(v Valued, currency num.Currency) (num.Money, error)
}

// Ratio is the v1 Policy: every position requires |notional| × ratio
// (initial_margin_ratio). 1.0 is unlevered, 0.5 permits 2× gross
// exposure, 0.25 permits 4×.
type Ratio struct {
	ratio num.Rate
}

// NewRatio returns a Ratio policy. ratio must be positive.
func NewRatio(ratio num.Rate) (Ratio, error) {
	if ratio.Sign() <= 0 {
		return Ratio{}, fmt.Errorf("%w: initial margin ratio must be positive, got %s", ErrInvalidPolicy, ratio)
	}
	return Ratio{ratio: ratio}, nil
}

// Value returns the configured ratio.
func (r Ratio) Value() num.Rate { return r.ratio }

// Validate implements Policy. The zero Ratio, not built by NewRatio,
// reports ErrInvalidPolicy.
func (r Ratio) Validate() error {
	if r.ratio.Sign() <= 0 {
		return fmt.Errorf("%w: ratio must be constructed with NewRatio", ErrInvalidPolicy)
	}
	return nil
}

// RequiredMargin implements Policy.
func (r Ratio) RequiredMargin(v Valued, currency num.Currency) (num.Money, error) {
	if err := r.Validate(); err != nil {
		return num.Money{}, err
	}
	notional, err := Notional(v, currency)
	if err != nil {
		return num.Money{}, err
	}
	required, err := notional.MulRate(r.ratio)
	if err != nil {
		return num.Money{}, fmt.Errorf("margin: required margin of %s: %w", v.Listing.InstrumentID(), err)
	}
	return required, nil
}

// Marks maps each listing to its current valuation price. Marks are
// keyed by listing, not instrument: two listings of the same
// instrument (different provider or venue) are different positions
// and can carry different marks.
type Marks map[account.ListingKey]num.Price

// Requirement is an account's gross notional exposure and the initial
// margin it requires, both in the account currency.
type Requirement struct {
	Gross    num.Money
	Required num.Money
}

// Within reports whether the required margin does not exceed equity —
// the ADR-066 admission invariant. Exact equality is within the limit.
func (r Requirement) Within(equity num.Money) (bool, error) {
	cmp, err := r.Required.Cmp(equity)
	if err != nil {
		return false, fmt.Errorf("margin: comparing required margin to equity: %w", err)
	}
	return cmp <= 0, nil
}

// Account returns the gross notional and required margin of positions,
// each open position valued at its listing's mark. Flat positions
// contribute nothing and need no mark. An open position with no mark
// reports ErrMissingMark.
func Account(positions []runtimeorder.Position, marks Marks, policy Policy, currency num.Currency) (Requirement, error) {
	acc, err := newAccumulator(policy, currency)
	if err != nil {
		return Requirement{}, err
	}
	for _, p := range positions {
		if err := acc.addMarked(p, marks); err != nil {
			return Requirement{}, err
		}
	}
	return acc.req, nil
}

// Change describes one listing's position after a proposal or fill.
type Change struct {
	// Listing identifies the changed position. Positions are matched by
	// account.ListingKey, the same identity at which an account holds
	// one net position.
	Listing instrument.Listing
	// Resulting is the position's magnitude after the change, as
	// computed with resulting-position semantics by the caller: a
	// 100-long position reversed by a 200-unit sell has a Resulting of
	// 100, not 200 or 300. Zero means the change closes the position.
	Resulting num.Quantity
	// Price values the changed listing in both the current and the
	// prospective state: the reference price at admission, or the
	// actual fill price at fill time.
	Price num.Price
}

// Assessment compares an account's requirement before and after one
// Change.
type Assessment struct {
	Current     Requirement
	Prospective Requirement
}

// Increases reports whether the change raises gross notional. A change
// that doesn't is de-risking in ADR-066's sense.
//
// It compares gross notional, not required margin: required margin is
// rounded (Money × Rate), so a small real increase in gross exposure can
// round to the same required margin and must still count as an
// increase.
func (a Assessment) Increases() (bool, error) {
	cmp, err := a.Prospective.Gross.Cmp(a.Current.Gross)
	if err != nil {
		return false, fmt.Errorf("margin: comparing prospective to current gross notional: %w", err)
	}
	return cmp > 0, nil
}

// Assess returns the account's requirement before and after change.
//
// The changed listing is valued at change.Price in both states, so the
// two differ only in its quantity. Every other open position — including
// another listing of the same instrument — is valued at its mark in both
// states and needs one; the changed listing does not. More than one open
// position in the changed listing reports ErrInvalidInput.
func Assess(positions []runtimeorder.Position, marks Marks, change Change, policy Policy, currency num.Currency) (Assessment, error) {
	if change.Listing.InstrumentID().IsZero() {
		return Assessment{}, fmt.Errorf("%w: change listing must be constructed", ErrInvalidInput)
	}
	current, err := newAccumulator(policy, currency)
	if err != nil {
		return Assessment{}, err
	}
	prospective, err := newAccumulator(policy, currency)
	if err != nil {
		return Assessment{}, err
	}

	changedKey := account.KeyOf(change.Listing)
	matched := false
	for _, p := range positions {
		if p.Quantity.IsZero() {
			continue
		}
		if account.KeyOf(p.Listing) == changedKey {
			if matched {
				return Assessment{}, fmt.Errorf("%w: more than one open position in listing %s/%s/%s",
					ErrInvalidInput, changedKey.InstrumentID, changedKey.Provider, changedKey.Venue)
			}
			matched = true
			if err := current.add(Valued{Listing: p.Listing, Quantity: p.Quantity, Price: change.Price}); err != nil {
				return Assessment{}, err
			}
			continue
		}
		if err := current.addMarked(p, marks); err != nil {
			return Assessment{}, err
		}
		if err := prospective.addMarked(p, marks); err != nil {
			return Assessment{}, err
		}
	}
	if err := prospective.add(Valued{Listing: change.Listing, Quantity: change.Resulting, Price: change.Price}); err != nil {
		return Assessment{}, err
	}
	return Assessment{Current: current.req, Prospective: prospective.req}, nil
}

// accumulator sums gross notional and required margin in one currency.
type accumulator struct {
	policy   Policy
	currency num.Currency
	req      Requirement
}

func newAccumulator(policy Policy, currency num.Currency) (*accumulator, error) {
	if policy == nil {
		return nil, fmt.Errorf("%w: policy must be set", ErrInvalidPolicy)
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	zero, err := num.ParseMoney("0", currency)
	if err != nil {
		return nil, fmt.Errorf("%w: account currency: %v", ErrInvalidInput, err)
	}
	return &accumulator{policy: policy, currency: currency, req: Requirement{Gross: zero, Required: zero}}, nil
}

// addMarked adds p valued at its listing's mark. Flat positions are
// skipped.
func (a *accumulator) addMarked(p runtimeorder.Position, marks Marks) error {
	if p.Quantity.IsZero() {
		return nil
	}
	key := account.KeyOf(p.Listing)
	mark, ok := marks[key]
	if !ok {
		return fmt.Errorf("%w: no mark for open position in listing %s/%s/%s",
			ErrMissingMark, key.InstrumentID, key.Provider, key.Venue)
	}
	return a.add(Valued{Listing: p.Listing, Quantity: p.Quantity, Price: mark})
}

func (a *accumulator) add(v Valued) error {
	if v.Quantity.IsZero() {
		// A closing change contributes nothing, but a mismatched
		// listing is still unusable input.
		return checkListing(v.Listing, a.currency)
	}
	notional, err := Notional(v, a.currency)
	if err != nil {
		return err
	}
	required, err := a.policy.RequiredMargin(v, a.currency)
	if err != nil {
		return err
	}
	if a.req.Gross, err = a.req.Gross.Add(notional); err != nil {
		return fmt.Errorf("margin: summing gross notional: %w", err)
	}
	if a.req.Required, err = a.req.Required.Add(required); err != nil {
		return fmt.Errorf("margin: summing required margin: %w", err)
	}
	return nil
}

// checkListing reports whether l is constructed and settles in
// currency.
func checkListing(l instrument.Listing, currency num.Currency) error {
	if l.InstrumentID().IsZero() {
		return fmt.Errorf("%w: listing must be constructed", ErrInvalidInput)
	}
	if settle := l.Spec().SettlementCurrency(); !settle.Equal(currency) {
		return fmt.Errorf("%w: listing %s settles in %s, account currency is %s",
			ErrCurrencyMismatch, l.InstrumentID(), settle, currency)
	}
	return nil
}
