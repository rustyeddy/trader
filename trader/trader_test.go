package trader

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/data"
	tlog "github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
)

func TestTrader(t *testing.T) {
	err := tlog.Setup(tlog.Config{Level: "debug", Format: "text"})
	assert.NoError(t, err)

	instrument := "EURUSD"

	start := time.Date(2022, time.Month(time.January), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, time.Month(time.January), 0, 0, 0, 0, 0, time.UTC)

	cfg := &ConfigBackTest{
		Instrument: instrument,
		Strategy:   "fake",
		Start:      start,
		End:        end,
		TimeFrame:  types.M1,
		Account:    "test",
	}

	am := account.NewAccountManager()
	trader := Trader{
		Account:     am.CreateAccount("test", 1000),
		DataManager: data.NewDataManager([]string{"EURUSD"}, start, end),
		Broker: &broker.Broker{
			ID: types.NewULID(),
			OpenOrders: broker.OpenOrders{
				Orders: make(map[string]*types.Order),
			},
		},
	}

	ctx := context.TODO()
	err = trader.BackTest(ctx, cfg)
	assert.NoError(t, err)
}
