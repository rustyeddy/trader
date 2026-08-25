package execution

import (
	"fmt"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
)

// Deps are a Planner's injected dependencies (ADR-015): the clock and
// ID generator a produced Proposal's own Metadata (EventID, Timestamp)
// are derived from, deterministically. Plan never calls time.Now or a
// global ID source directly — see Planner's own doc comment for why
// this matters for determinism.
type Deps struct {
	Clock clock.Clock
	IDs   *id.Generator
}

func (d Deps) validate() error {
	if d.Clock == nil {
		return fmt.Errorf("%w: clock must be set", ErrInvalidDeps)
	}
	if d.IDs == nil {
		return fmt.Errorf("%w: id generator must be set", ErrInvalidDeps)
	}
	return nil
}
