package trader

type StrategyPlan struct {
	Opens  []*OpenRequest
	Closes []*closeRequest
	Cancel []string
	Reason string
}

var DefaultStrategyPlan = StrategyPlan{
	Reason: "hold",
}
