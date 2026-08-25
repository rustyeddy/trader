package execution

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// planner is the v0 reference Planner implementation (#179, M4-04):
// direct translation of IntentEnter/IntentExit/IntentTargetExposure
// into exactly one Proposal. It always plans a Market/GTC order —
// Intent carries no limit-price hint (#177), so there is nothing to
// plan a Limit or Stop order from yet; that is additive future work
// once a real consumer needs it. IntentAdjustStop is not supported —
// see ErrUnsupportedIntentKind's own doc comment.
type planner struct {
	deps Deps
}

// NewPlanner returns a Planner backed by deps. Both deps.Clock and
// deps.IDs must be set.
func NewPlanner(deps Deps) (Planner, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	return &planner{deps: deps}, nil
}

// Plan implements Planner.
func (p *planner) Plan(ctx context.Context, in PlanInput) (PlanResult, error) {
	if err := ctx.Err(); err != nil {
		return PlanResult{}, err
	}

	intent, err := checkPlanInput(in)
	if err != nil {
		return PlanResult{}, err
	}

	var side order.Side
	var qty num.Quantity
	var reduceOnly bool

	switch intent.Kind {
	case order.IntentEnter:
		side, qty = intent.Side, *in.Quantity
	case order.IntentExit:
		side, qty, err = planExit(in.Account, in.Listing)
		reduceOnly = true
	case order.IntentTargetExposure:
		side, qty, reduceOnly, err = planTargetExposure(in.Account, in.Listing, intent.Side, *intent.Quantity)
	default:
		err = fmt.Errorf("%w: %v", ErrUnsupportedIntentKind, intent.Kind)
	}
	if err != nil {
		return PlanResult{}, err
	}

	eventID, err := id.GenerateEventID(p.deps.IDs)
	if err != nil {
		return PlanResult{}, err
	}

	proposal, err := order.NewProposal(order.Proposal{
		Listing:     in.Listing,
		AccountID:   in.Account.AccountID(),
		Side:        side,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    qty,
		ReduceOnly:  reduceOnly,
		Metadata: id.Metadata{
			EventID:       eventID,
			CorrelationID: intent.Metadata.CorrelationID,
			CausationID:   intent.Metadata.EventID,
			Timestamp:     p.deps.Clock.Now(),
		},
	})
	if err != nil {
		return PlanResult{}, fmt.Errorf("execution: building proposal: %w", err)
	}
	return PlanResult{Proposal: proposal}, nil
}
