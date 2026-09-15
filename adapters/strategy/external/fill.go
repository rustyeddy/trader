package external

import (
	"fmt"

	"github.com/rustyeddy/trader/account"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/strategy"
)

// ToWireFillEvent builds the *v1.FillEvent the host writes down the
// Run stream for one strategy.FillEvent, at sequence, when
// CAPABILITY_FILL_HANDLER was negotiated (ADR-060). Like
// AccountSnapshot, this is a deliberately minimal slice of order.Fill
// (order_id/instrument_id/side/price/quantity/correlation_id/
// causation_id) — FillID, BrokerOrderID, BrokerFillID, Timestamp, and
// Commission are not carried, so there is no FromWireFillEvent (see
// doc.go).
func ToWireFillEvent(sequence uint64, event strategy.FillEvent, acct account.Snapshot) (*v1.FillEvent, error) {
	f := event.Fill
	side, err := toWireSide(f.Side)
	if err != nil {
		return nil, fmt.Errorf("external: fill event: %w", err)
	}
	wireAcct, err := ToWireAccountSnapshot(acct)
	if err != nil {
		return nil, fmt.Errorf("external: fill event: %w", err)
	}
	return &v1.FillEvent{
		Sequence:      sequence,
		OrderId:       f.OrderID.String(),
		InstrumentId:  f.Listing.InstrumentID().String(),
		Side:          side,
		Price:         f.Price.String(),
		Quantity:      f.Quantity.String(),
		CorrelationId: correlationIDOrEmpty(f.Metadata.CorrelationID),
		CausationId:   eventIDOrEmpty(f.Metadata.CausationID),
		Account:       wireAcct,
	}, nil
}
