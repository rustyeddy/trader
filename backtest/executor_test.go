package backtest

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/types"
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

	exec.DataManager = datamanager.GetDataManager()
	require.ErrorContains(t, exec.Execute(context.Background(), run), "nil broker factory")

	exec.BrokerFactory = func() *account.Ledger { return nil }
	exec.AccountFactory = func(name string, balance types.Money) *account.Account { return account.NewAccount(name, balance) }
	require.ErrorContains(t, exec.Execute(context.Background(), run), "nil broker")

	exec.BrokerFactory = func() *account.Ledger { return account.NewLedger("sim") }
	exec.AccountFactory = nil
	require.ErrorContains(t, exec.Execute(context.Background(), run), "nil account factory")

	exec.AccountFactory = func(name string, balance types.Money) *account.Account { return nil }
	require.ErrorContains(t, exec.Execute(context.Background(), run), "nil account")
}

func TestNewTraderBacktestExecutor_DefaultFactories(t *testing.T) {
	t.Parallel()

	exec := NewTraderBacktestExecutor(datamanager.GetDataManager())
	require.NotNil(t, exec)
	require.NotNil(t, exec.BrokerFactory)
	require.NotNil(t, exec.AccountFactory)

	broker := exec.BrokerFactory()
	require.NotNil(t, broker)
	assert.Equal(t, "sim", broker.Name)

	acct := exec.AccountFactory("backtest", types.MoneyFromFloat(10_000))
	require.NotNil(t, acct)
	assert.Equal(t, "backtest", acct.Name)
	assert.Equal(t, types.MoneyFromFloat(10_000), acct.Balance)
}
