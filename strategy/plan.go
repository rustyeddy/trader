package strategy

import "github.com/rustyeddy/trader/account"

type StrategyPlan struct {
	Opens  []*account.OpenRequest
	Closes []*account.CloseRequest
	Cancel []string
	Reason string
}

// Empty reports whether the plan has no actions to execute.
func (p *StrategyPlan) Empty() bool {
	if p == nil {
		return true
	}
	return len(p.Opens) == 0 && len(p.Closes) == 0 && len(p.Cancel) == 0
}
