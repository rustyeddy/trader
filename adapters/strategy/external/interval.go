package external

import (
	"fmt"

	"github.com/rustyeddy/trader/marketdata"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// ToWireInterval converts iv to its v1 wire form. The host sends this
// inline in BarEvent and DataRequirement (Descriptor).
func ToWireInterval(iv marketdata.Interval) (*v1.Interval, error) {
	unit, err := toWireIntervalUnit(iv.Unit())
	if err != nil {
		return nil, fmt.Errorf("external: interval: %w", err)
	}
	return &v1.Interval{Unit: unit, Count: int32(iv.Count())}, nil
}

// FromWireInterval reconstructs a marketdata.Interval from a received
// *v1.Interval — GetHistoryBarsRequest.interval or a Handshake
// DataRequirement.interval.
func FromWireInterval(w *v1.Interval) (marketdata.Interval, error) {
	if w == nil {
		return marketdata.Interval{}, fmt.Errorf("%w: interval must be set", ErrInvalidWireValue)
	}
	unit, err := fromWireIntervalUnit(w.GetUnit())
	if err != nil {
		return marketdata.Interval{}, fmt.Errorf("external: interval: %w", err)
	}
	iv, err := marketdata.NewInterval(unit, int(w.GetCount()))
	if err != nil {
		return marketdata.Interval{}, fmt.Errorf("%w: interval: %v", ErrInvalidWireValue, err)
	}
	return iv, nil
}
