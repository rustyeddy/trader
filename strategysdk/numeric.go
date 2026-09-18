package strategysdk

import (
	"fmt"

	"github.com/rustyeddy/trader/num"
)

// This file parses the plain decimal-text exact values strategy.proto's
// own "Common value types" section documents into num.Price/
// num.Quantity — never through a float64, matching num's own
// ParsePrice/ParseQuantity checked-decimal-text contract and ADR-004.

// parsePrice parses a required price field.
func parsePrice(field, s string) (num.Price, error) {
	if s == "" {
		return num.Price{}, fmt.Errorf("%w: %s must not be empty", ErrInvalidWireValue, field)
	}
	p, err := num.ParsePrice(s)
	if err != nil {
		return num.Price{}, fmt.Errorf("%w: %s: %v", ErrInvalidWireValue, field, err)
	}
	return p, nil
}

// parseOptionalPrice parses a price field the wire schema documents
// as empty-means-not-applicable (for example PositionSnapshot.
// avg_price when flat). It returns nil, not the zero Price, for an
// empty string, matching order.Position.AvgPrice's own convention.
func parseOptionalPrice(field, s string) (*num.Price, error) {
	if s == "" {
		return nil, nil
	}
	p, err := parsePrice(field, s)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// parseQuantity parses a required quantity field.
func parseQuantity(field, s string) (num.Quantity, error) {
	if s == "" {
		return num.Quantity{}, fmt.Errorf("%w: %s must not be empty", ErrInvalidWireValue, field)
	}
	q, err := num.ParseQuantity(s)
	if err != nil {
		return num.Quantity{}, fmt.Errorf("%w: %s: %v", ErrInvalidWireValue, field, err)
	}
	return q, nil
}

// priceOrEmpty formats p for a wire field, returning "" when p is nil
// (the "not applicable" convention every exact-value wire field
// documents).
func priceOrEmpty(p *num.Price) string {
	if p == nil {
		return ""
	}
	return p.String()
}

// quantityOrEmpty is priceOrEmpty's num.Quantity counterpart.
func quantityOrEmpty(q *num.Quantity) string {
	if q == nil {
		return ""
	}
	return q.String()
}
