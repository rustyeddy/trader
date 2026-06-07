package trader

import (
	"context"
	"fmt"
)

// BacktestExecutor runs an executable backtest using whatever runtime
// dependencies it needs. Service-layer code depends on this narrow contract
// instead of constructing Trader/Broker/Account directly.
type BacktestExecutor interface {
	Execute(context.Context, *Backtest) error
}

// TraderBacktestExecutor executes a Backtest by wiring it through Trader with
// factory-provided runtime dependencies.
type TraderBacktestExecutor struct {
	DataManager    CandleSource
	BrokerFactory  func() *Broker
	AccountFactory func(name string, balance Money) *Account
}

// NewTraderBacktestExecutor returns a BacktestExecutor that uses Trader as the
// concrete execution engine.
func NewTraderBacktestExecutor(dm CandleSource) *TraderBacktestExecutor {
	return &TraderBacktestExecutor{
		DataManager: dm,
		BrokerFactory: func() *Broker {
			return NewBroker("sim")
		},
		AccountFactory: func(name string, balance Money) *Account {
			return NewAccount(name, balance)
		},
	}
}

// Execute runs one backtest with freshly-constructed runtime dependencies.
func (e *TraderBacktestExecutor) Execute(ctx context.Context, run *Backtest) error {
	if e == nil {
		return fmt.Errorf("nil backtest executor")
	}
	if run == nil || run.BacktestRequest == nil {
		return fmt.Errorf("nil backtest run")
	}
	if e.DataManager == nil {
		return fmt.Errorf("nil data manager")
	}
	if e.BrokerFactory == nil {
		return fmt.Errorf("nil broker factory")
	}
	if e.AccountFactory == nil {
		return fmt.Errorf("nil account factory")
	}

	t := &Trader{DataManager: e.DataManager}
	t.Broker = e.BrokerFactory()
	if t.Broker == nil {
		return fmt.Errorf("nil broker")
	}
	acct := e.AccountFactory("backtest", run.StartingBalance)
	if acct == nil {
		return fmt.Errorf("nil account")
	}
	if run.RiskPct != 0 {
		acct.RiskPct = run.RiskPct
	}
	t.Broker.Account = acct

	return t.Backtest(ctx, run)
}
