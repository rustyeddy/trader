package trader

import (
	"testing"

	"github.com/rustyeddy/trader/execution"
	"github.com/stretchr/testify/assert"
)

func TestBacktestRunGetTrades(t *testing.T) {
	t.Parallel()

	var nilRun *BacktestRun
	assert.Nil(t, nilRun.GetTrades())

	trades := []*execution.Trade{{PNL: MoneyFromFloat(100)}, nil, {PNL: MoneyFromFloat(-25)}}
	run := &BacktestRun{Trades: trades}
	assert.Equal(t, trades, run.GetTrades())
}
