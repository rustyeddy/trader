package backtest

import (
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
)

func TestBacktestRunGetTrades(t *testing.T) {
	t.Parallel()

	var nilRun *BacktestRun
	assert.Nil(t, nilRun.GetTrades())

	trades := []*account.Trade{{PNL: types.MoneyFromFloat(100)}, nil, {PNL: types.MoneyFromFloat(-25)}}
	run := &BacktestRun{Trades: trades}
	assert.Equal(t, trades, run.GetTrades())
}
