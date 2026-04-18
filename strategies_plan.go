package trader

import "github.com/rustyeddy/trader/types"

type StrategyPlan struct {
	Opens  []*types.OpenRequest
	Closes []*types.CloseRequest
	Cancel []string
	Reason string
}

var DefaultStrategyPlan = StrategyPlan{
	Reason: "hold",
}
