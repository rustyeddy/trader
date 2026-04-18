package trader

// Strategy is the interface for candle-based strategies.
type Strategy interface {
	Name() string
	Reset()
	Ready() bool
	Update(c Candle) *StrategyPlan
}

type StrategyBaseConfig struct {
	Instrument string
}
