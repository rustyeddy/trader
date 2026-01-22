// pkg/sim/triggers.go
package sim

func hitStopLoss(t *Trade, price float64) bool {
	if t.StopLoss == nil {
		return false
	}
	if t.Units > 0 {
		return price <= *t.StopLoss
	}
	return price >= *t.StopLoss
}

func hitTakeProfit(t *Trade, price float64) bool {
	if t.TakeProfit == nil {
		return false
	}
	if t.Units > 0 {
		return price >= *t.TakeProfit
	}
	return price <= *t.TakeProfit
}
