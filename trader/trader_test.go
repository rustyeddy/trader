package trader

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/data"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
)

func TestTrader(t *testing.T) {
	start := time.Date(2022, time.Month(time.January), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, time.Month(time.January), 0, 0, 0, 0, 0, time.UTC)
	am := account.NewAccountManager()
	trader := Trader{
		AccountManager: am,
		DataManager:    data.NewDataManager([]string{"EURUSD"}, start, end),
	}

	cfg := ConfigBackTest{
		Instrument: market.GetInstrument("EURUSD"),
		Account:    am.CreateAccount("test", types.MoneyFromFloat(1000)),
	}

	ctx := context.TODO()
	err := trader.BackTest(ctx, cfg)
	assert.NoError(t, err)
}
