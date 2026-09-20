package order

import (
	"fmt"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
)

// validatePricePresence reports an error unless limit and stop are
// present exactly when t requires them: neither for Market, limit only
// for Limit, stop only for Stop, both for StopLimit. It is shared by
// Proposal's requested prices and Order's accepted prices so the two
// stay consistent with each other.
func validatePricePresence(t Type, limit, stop *num.Price) error {
	if t.requiresLimitPrice() && limit == nil {
		return fmt.Errorf("%s order requires a limit price", t)
	}
	if !t.requiresLimitPrice() && limit != nil {
		return fmt.Errorf("%s order must not have a limit price", t)
	}
	if t.requiresStopPrice() && stop == nil {
		return fmt.Errorf("%s order requires a stop price", t)
	}
	if !t.requiresStopPrice() && stop != nil {
		return fmt.Errorf("%s order must not have a stop price", t)
	}
	return nil
}

// validatePriceAndQuantity reports an error if qty is not a positive
// multiple of listing's quantity increment, or if a non-nil limit/stop
// price is not an exact multiple of listing's tick size.
func validatePriceAndQuantity(listing instrument.Listing, qty num.Quantity, limit, stop *num.Price) error {
	if qty.IsZero() {
		return fmt.Errorf("quantity must be greater than zero")
	}
	if err := listing.Spec().ValidateQuantity(qty); err != nil {
		return err
	}
	if limit != nil {
		if err := listing.Spec().ValidatePrice(*limit); err != nil {
			return fmt.Errorf("limit price: %w", err)
		}
	}
	if stop != nil {
		if err := listing.Spec().ValidatePrice(*stop); err != nil {
			return fmt.Errorf("stop price: %w", err)
		}
	}
	return nil
}
