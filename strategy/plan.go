package strategy

import "github.com/rustyeddy/trader/execution"

type StrategyPlan struct {
	Opens  []*execution.OpenRequest
	Closes []*execution.CloseRequest
	Cancel []string
	Reason string
}

var DefaultStrategyPlan = StrategyPlan{
	Reason: "hold",
}

// DefaultPlan returns a fresh no-op plan with the default hold reason.
func DefaultPlan() *StrategyPlan {
	plan := DefaultStrategyPlan
	return &plan
}

// HoldPlan returns a fresh no-op plan with the provided reason.
// An empty reason falls back to the default hold reason.
func HoldPlan(reason string) *StrategyPlan {
	plan := DefaultPlan()
	if reason != "" {
		plan.Reason = reason
	}
	return plan
}

// Empty reports whether the plan has no actions to execute.
func (p *StrategyPlan) Empty() bool {
	if p == nil {
		return true
	}
	return len(p.Opens) == 0 && len(p.Closes) == 0 && len(p.Cancel) == 0
}
