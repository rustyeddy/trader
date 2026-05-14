package trader

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountPrint(t *testing.T) {
	t.Parallel()

	acct := NewAccount("test", MoneyFromFloat(50_000))
	acct.Balance = MoneyFromFloat(50_500)
	acct.Equity = MoneyFromFloat(50_750)

	require.NotPanics(t, func() {
		acct.Print()
	})
}

