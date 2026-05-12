package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBacktestRunBuildBacktestResult_Guards(t *testing.T) {
	t.Parallel()

	acct := &Account{Trades: []*Trade{{PNL: MoneyFromFloat(10)}}}
	var nilRun *BacktestRun

	assert.NotPanics(t, func() {
		nilRun.BuildBacktestResult(acct)
	})

	run := &BacktestRun{}
	assert.NotPanics(t, func() {
		run.BuildBacktestResult(nil)
	})
	assert.Nil(t, run.Trades)
}

func TestBacktestRunBuildBacktestResult_CopiesTrades(t *testing.T) {
	t.Parallel()

	run := &BacktestRun{Trades: []*Trade{{PNL: MoneyFromFloat(1)}, {PNL: MoneyFromFloat(2)}}}
	acct := &Account{Trades: []*Trade{{PNL: MoneyFromFloat(100)}, nil, {PNL: MoneyFromFloat(-25)}}}

	run.BuildBacktestResult(acct)
	assert.Len(t, run.Trades, 3)
	assert.Equal(t, acct.Trades[0], run.Trades[0])
	assert.Nil(t, run.Trades[1])
	assert.Equal(t, acct.Trades[2], run.Trades[2])

	acct.Trades = []*Trade{{PNL: MoneyFromFloat(7)}}
	run.BuildBacktestResult(acct)
	assert.Len(t, run.Trades, 1)
	assert.Equal(t, acct.Trades[0], run.Trades[0])
}
