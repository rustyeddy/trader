package sdk

import (
	"fmt"
	"math"

	"github.com/rustyeddy/trader/marketdata"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// toWireInterval converts iv to its v1 wire form — used to send
// DataRequirement.interval and GetHistoryBarsRequest.interval.
func toWireInterval(iv marketdata.Interval) (*v1.Interval, error) {
	unit, err := toWireIntervalUnit(iv.Unit())
	if err != nil {
		return nil, fmt.Errorf("sdk: interval: %w", err)
	}
	if iv.Count() > math.MaxInt32 {
		return nil, fmt.Errorf("%w: interval: count %d exceeds int32 range", ErrInvalidWireValue, iv.Count())
	}
	return &v1.Interval{Unit: unit, Count: int32(iv.Count())}, nil
}

// fromWireInterval reconstructs a marketdata.Interval from a received
// *v1.Interval — BarEvent.interval.
func fromWireInterval(w *v1.Interval) (marketdata.Interval, error) {
	if w == nil {
		return marketdata.Interval{}, fmt.Errorf("%w: interval must be set", ErrInvalidWireValue)
	}
	unit, err := fromWireIntervalUnit(w.GetUnit())
	if err != nil {
		return marketdata.Interval{}, fmt.Errorf("sdk: interval: %w", err)
	}
	iv, err := marketdata.NewInterval(unit, int(w.GetCount()))
	if err != nil {
		return marketdata.Interval{}, fmt.Errorf("%w: interval: %v", ErrInvalidWireValue, err)
	}
	return iv, nil
}
