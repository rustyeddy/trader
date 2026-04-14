package strategies

import "github.com/rustyeddy/trader/types"

type Plan struct {
	Opens  []*types.OpenRequest
	Closes []*types.CloseRequest
	Cancel []string
	Reason string
}

var DefaultPlan = Plan{
	Reason: "hold",
}
