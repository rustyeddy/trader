package external

import (
	"fmt"

	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/strategy"
)

// FromWireDescriptor reconstructs a strategy.Descriptor from the
// StrategyDescriptor a guest carried in its HandshakeRequest.
// ExternalStrategyAdapter (#379) answers its own Describe() from this
// value, retained from Handshake — no further RPC round trip.
func FromWireDescriptor(w *v1.StrategyDescriptor) (strategy.Descriptor, error) {
	if w == nil {
		return strategy.Descriptor{}, fmt.Errorf("%w: strategy descriptor must be set", ErrInvalidWireValue)
	}

	reqs := make([]strategy.DataRequirement, 0, len(w.GetRequirements()))
	for i, wr := range w.GetRequirements() {
		req, err := fromWireDataRequirement(wr)
		if err != nil {
			return strategy.Descriptor{}, fmt.Errorf("external: descriptor: requirement %d: %w", i, err)
		}
		reqs = append(reqs, req)
	}

	return strategy.Descriptor{
		Name:         w.GetName(),
		Version:      w.GetVersion(),
		Requirements: reqs,
	}, nil
}

func fromWireDataRequirement(w *v1.DataRequirement) (strategy.DataRequirement, error) {
	if w == nil {
		return strategy.DataRequirement{}, fmt.Errorf("%w: data requirement must be set", ErrInvalidWireValue)
	}
	instID, err := parseInstrumentID(w.GetInstrumentId())
	if err != nil {
		return strategy.DataRequirement{}, err
	}
	interval, err := FromWireInterval(w.GetInterval())
	if err != nil {
		return strategy.DataRequirement{}, err
	}
	if w.GetWarmupBars() < 0 {
		return strategy.DataRequirement{}, fmt.Errorf("%w: warmup_bars must not be negative", ErrInvalidWireValue)
	}
	return strategy.DataRequirement{
		Instrument: instID,
		Interval:   interval,
		WarmupBars: int(w.GetWarmupBars()),
	}, nil
}
