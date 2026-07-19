package backtest

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/engine"
	"github.com/rustyeddy/trader/types"
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
	DataManager    engine.CandleSource
	BrokerFactory  func() *account.Broker
	AccountFactory func(name string, balance types.Money) *account.Account
}

// NewTraderBacktestExecutor returns a BacktestExecutor that uses Trader as the
// concrete execution engine.
func NewTraderBacktestExecutor(dm engine.CandleSource) *TraderBacktestExecutor {
	return &TraderBacktestExecutor{
		DataManager: dm,
		BrokerFactory: func() *account.Broker {
			return account.NewBroker("sim")
		},
		AccountFactory: func(name string, balance types.Money) *account.Account {
			return account.NewAccount(name, balance)
		},
	}
}

// Execute runs one backtest with freshly-constructed runtime dependencies.
func (e *TraderBacktestExecutor) Execute(ctx context.Context, run *Backtest) error {
	if e == nil {
		return fmt.Errorf("nil backtest executor")
	}
	if run == nil || run.Request == nil {
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

	t := &engine.Trader{DataManager: e.DataManager}
	t.Broker = e.BrokerFactory()
	if t.Broker == nil {
		return fmt.Errorf("nil broker")
	}
	acct := e.AccountFactory("backtest", run.Request.StartingBalance)
	if acct == nil {
		return fmt.Errorf("nil account")
	}
	if run.Request.RiskPct != 0 {
		acct.RiskFraction = run.Request.RiskPct
	}
	t.Broker.Account = acct

	return run.Execute(ctx, t)
}
