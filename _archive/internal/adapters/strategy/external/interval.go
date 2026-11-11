package external

import (
	"fmt"
	"math"

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
	// marketdata.Interval stores Count as a Go int (64 bits on every
	// platform this module targets); the wire field is a proto int32.
	// Casting a count above math.MaxInt32 would silently wrap into an
	// unrelated, possibly negative wire value instead of failing
	// explicitly (review finding) — reject it before casting rather
	// than after.
	if iv.Count() > math.MaxInt32 {
		return nil, fmt.Errorf("%w: interval: count %d exceeds int32 range", ErrInvalidWireValue, iv.Count())
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
