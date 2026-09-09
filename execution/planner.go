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
// into exactly one Market/GTC Proposal. IntentAdjustStop (issue #336)
// is the one exception: it plans a Stop/GTC, ReduceOnly Proposal
// instead, sized to close the account's entire current position —
// but only for *initial* stop placement, when no resting Stop order
// already exists for the instrument. Ratcheting an existing stop is a
// replacement, not a new-order Proposal; see PlanReplace and
// ErrExistingStopOrder.
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
	orderType := order.Market
	var stopPrice *num.Price

	switch intent.Kind {
	case order.IntentEnter:
		side, qty = intent.Side, *in.Quantity
	case order.IntentExit:
		side, qty, err = planExit(in.Account, in.Listing)
		reduceOnly = true
	case order.IntentTargetExposure:
		side, qty, reduceOnly, err = planTargetExposure(in.Account, in.Listing, intent.Side, *intent.Quantity)
	case order.IntentAdjustStop:
		if _, exists := findRestingStopOrder(in.Account, in.Listing); exists {
			err = fmt.Errorf("%w: %v", ErrExistingStopOrder, intent.Instrument)
			break
		}
		side, qty, err = planAdjustStop(in.Account, in.Listing)
		reduceOnly = true
		orderType = order.Stop
		stopPrice = intent.StopPrice
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
		Type:        orderType,
		TimeInForce: order.GTC,
		Quantity:    qty,
		StopPrice:   stopPrice,
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
