package pipeline

import (
	"fmt"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/risk"
)

// Deps are a Pipeline's injected dependencies (ADR-015): every stage
// of the canonical M4 path, plus the ID generator Submit uses to
// assign the order.OrderID a risk-approved Proposal is submitted
// under. Sizer is required even though not every Intent.Kind uses it
// (only order.IntentEnter does) — making it mandatory at construction
// avoids a runtime "no sizer configured" surprise for a Pipeline that
// otherwise looks fully wired (#185 design discussion).
type Deps struct {
	Sizer   risk.Sizer
	Planner execution.Planner
	Engine  risk.Engine
	Broker  brokerpkg.Broker
	IDs     *id.Generator
}

func (d Deps) validate() error {
	if d.Sizer == nil {
		return fmt.Errorf("%w: sizer must be set", ErrInvalidDeps)
	}
	if d.Planner == nil {
		return fmt.Errorf("%w: planner must be set", ErrInvalidDeps)
	}
	if d.Engine == nil {
		return fmt.Errorf("%w: engine must be set", ErrInvalidDeps)
	}
	if d.Broker == nil {
		return fmt.Errorf("%w: broker must be set", ErrInvalidDeps)
	}
	if d.IDs == nil {
		return fmt.Errorf("%w: id generator must be set", ErrInvalidDeps)
	}
	return nil
}
