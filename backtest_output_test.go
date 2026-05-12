package trader

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrintBacktest_NoPanicWithNilWriter(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		PrintBacktest(nil, BacktestResult{})
	})
}

func TestPrintBacktest_CurrentlyNoOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := BacktestResult{
		Balance: MoneyFromFloat(10000),
		Trades:  4,
		Wins:    2,
		Losses:  1,
		Flat:    1,
	}

	PrintBacktest(&buf, r)
	assert.Equal(t, "", buf.String())
}
