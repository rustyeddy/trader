package strategies

import "github.com/rustyeddy/trader/portfolio"

type Plan struct {
	Opens  []*portfolio.OpenRequest
	Closes []*portfolio.CloseRequest
	Cancel []string
	Reason string
}

var DefaultPlan = Plan{
	Reason: "hold",
}
