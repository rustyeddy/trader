package trader

type StrategyPlan struct {
	Opens  []*OpenRequest
	Closes []*CloseRequest
	Cancel []string
	Reason string
}

var DefaultStrategyPlan = StrategyPlan{
	Reason: "hold",
}
