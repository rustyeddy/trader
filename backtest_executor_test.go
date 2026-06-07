package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraderBacktestExecutor_Guards(t *testing.T) {
	t.Parallel()

	run := &Backtest{Request: &BacktestRequest{}}

	var nilExec *TraderBacktestExecutor
	require.ErrorContains(t, nilExec.Execute(context.Background(), run), "nil backtest executor")

	exec := &TraderBacktestExecutor{}
	require.ErrorContains(t, exec.Execute(context.Background(), run), "nil data manager")

	exec.DataManager = GetDataManager()
	require.ErrorContains(t, exec.Execute(context.Background(), run), "nil broker factory")

	exec.BrokerFactory = func() *Broker { return nil }
	exec.AccountFactory = func(name string, balance Money) *Account { return NewAccount(name, balance) }
	require.ErrorContains(t, exec.Execute(context.Background(), run), "nil broker")

	exec.BrokerFactory = func() *Broker { return NewBroker("sim") }
	exec.AccountFactory = nil
	require.ErrorContains(t, exec.Execute(context.Background(), run), "nil account factory")

	exec.AccountFactory = func(name string, balance Money) *Account { return nil }
	require.ErrorContains(t, exec.Execute(context.Background(), run), "nil account")
}

func TestNewTraderBacktestExecutor_DefaultFactories(t *testing.T) {
	t.Parallel()

	exec := NewTraderBacktestExecutor(GetDataManager())
	require.NotNil(t, exec)
	require.NotNil(t, exec.BrokerFactory)
	require.NotNil(t, exec.AccountFactory)

	broker := exec.BrokerFactory()
	require.NotNil(t, broker)
	assert.Equal(t, "sim", broker.ID)

	acct := exec.AccountFactory("backtest", MoneyFromFloat(10_000))
	require.NotNil(t, acct)
	assert.Equal(t, "backtest", acct.Name)
	assert.Equal(t, MoneyFromFloat(10_000), acct.Balance)
}
