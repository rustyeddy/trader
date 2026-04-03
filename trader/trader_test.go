package trader

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/data"
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

	am.CreateAccount("test", types.MoneyFromFloat(1000))
	cfg := ConfigBackTest{
		Instrument: "EURUSD",
		Account:    "test",
		TimeRange:  types.NewTimeRange(types.FromTime(start), types.FromTime(end)),
	}

	ctx := context.TODO()
	err := trader.BackTest(ctx, cfg)
	assert.NoError(t, err)
}
