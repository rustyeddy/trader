package instrument

import (
	"fmt"

	"github.com/rustyeddy/trader/num"
)

// Spec holds one Listing's exact trading mechanics: its minimum price
// increment, minimum quantity increment, contract multiplier, and the
// currency it settles in.
//
// Spec's fields are unexported; construct one with NewSpec. The Go zero
// value of Spec is not a valid, constructed Spec — NewListing rejects it.
type Spec struct {
	tickSize           num.Price
	quantityIncrement  num.Quantity
	multiplier         num.Rate
	settlementCurrency num.Currency
}

// NewSpec validates and returns a Spec. tickSize and quantityIncrement
// must be strictly positive, multiplier must be strictly positive
// (instruments with no meaningful contract multiplier, such as equities
// and currency pairs, pass num.MustParseRate("1")), and settlementCurrency
// must be structurally valid.
func NewSpec(tickSize num.Price, quantityIncrement num.Quantity, multiplier num.Rate, settlementCurrency num.Currency) (Spec, error) {
	if tickSize.IsZero() {
		return Spec{}, fmt.Errorf("%w: tick size must be positive", ErrInvalidSpec)
	}
	if quantityIncrement.IsZero() {
		return Spec{}, fmt.Errorf("%w: quantity increment must be positive", ErrInvalidSpec)
	}
	if multiplier.Sign() <= 0 {
		return Spec{}, fmt.Errorf("%w: multiplier must be positive", ErrInvalidSpec)
	}
	if !settlementCurrency.IsValid() {
		return Spec{}, fmt.Errorf("%w: settlement currency must be valid", ErrInvalidSpec)
	}
	return Spec{
		tickSize:           tickSize,
		quantityIncrement:  quantityIncrement,
		multiplier:         multiplier,
		settlementCurrency: settlementCurrency,
	}, nil
}

// TickSize returns s's minimum price increment.
func (s Spec) TickSize() num.Price { return s.tickSize }

// QuantityIncrement returns s's minimum quantity increment.
func (s Spec) QuantityIncrement() num.Quantity { return s.quantityIncrement }

// Multiplier returns s's contract multiplier.
func (s Spec) Multiplier() num.Rate { return s.multiplier }

// SettlementCurrency returns the currency s settles in.
func (s Spec) SettlementCurrency() num.Currency { return s.settlementCurrency }

// ValidatePrice reports an error unless price is an exact multiple of s's
// tick size, per ADR-004's exact price-increment rule. ValidatePrice
// reports ErrInvalidSpec, not num.ErrDivideByZero, if s is the
// unconstructed zero value.
func (s Spec) ValidatePrice(price num.Price) error {
	if s.tickSize.IsZero() {
		return fmt.Errorf("%w: spec must be constructed with NewSpec", ErrInvalidSpec)
	}
	ok, err := price.DivisibleBy(s.tickSize)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: price %s is not a multiple of tick size %s", ErrInvalidSpec, price, s.tickSize)
	}
	return nil
}

// ValidateQuantity reports an error unless qty is an exact multiple of s's
// quantity increment. ValidateQuantity reports ErrInvalidSpec, not
// num.ErrDivideByZero, if s is the unconstructed zero value.
func (s Spec) ValidateQuantity(qty num.Quantity) error {
	if s.quantityIncrement.IsZero() {
		return fmt.Errorf("%w: spec must be constructed with NewSpec", ErrInvalidSpec)
	}
	ok, err := qty.DivisibleBy(s.quantityIncrement)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: quantity %s is not a multiple of quantity increment %s", ErrInvalidSpec, qty, s.quantityIncrement)
	}
	return nil
}
