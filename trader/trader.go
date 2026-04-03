package trader

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/data"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategies"
	"github.com/rustyeddy/trader/types"
)

type Trader struct {
	*market.Instrument
	*account.AccountManager
	*data.DataManager
}

type ConfigBackTest struct {
	Instrument string
	Account    string
	types.TimeRange
}

func (t *Trader) BackTest(ctx context.Context, cfg ConfigBackTest) error {

	// Select an Account
	account := t.AccountManager.Get(cfg.Account)
	if account == nil {
		return fmt.Errorf("Account %s not found", cfg.Account)
	}

	// Select a Strategy and supply it with it's configuration
	strategy := strategies.Fake{
		StrategyConfig: strategies.StrategyConfig{
			Instrument: cfg.Instrument,
		},
		CandleCount: 10,
	}

	// Select the Instrument, TimeRange and TimeFrame
	timerange := cfg.TimeRange
	timerange.TF = types.M1
	candlereq := data.CandleRequest{
		Source:     "candles",
		Instrument: cfg.Instrument,
		Timeframe:  types.M1,
		Range:      timerange,
	}

	itr, err := t.DataManager.Candles(context.TODO(), candlereq)
	if err != nil {
		return err
	}

	for itr.Next() {
		candle := itr.Candle()

		// TODO This is a good time to check if any orders have been
		// filled from the broker.  Or other account / positioning
		// that needs to be made.

		plan := strategy.Update(ctx, &candle)
		for _, cancel := range plan.Cancel {
			fmt.Println("cancel: ", cancel)
		}

		for _, cl := range plan.Closes {

			// TODO
			//   Submit CloseRequest to broker
			//   Wait for closed Trade from broker
			//   Record the trade
			//   Close out Position
			//   Remove Positon from Account
			//   Add Closed trade to Account
			//   Update Account Balance, Equity and Margin
			//   Journal Trade
			//   Continue to next Candle

			fmt.Printf(" close: %v\n", cl)
		}

		for _, op := range plan.Opens {

			// get sizing from Account
			err := account.SizePosition(op)
			if err != nil {
				return err
			}
			fmt.Printf("  open: %v\n", op)
			// TODO
			//   Get position size from Account
			//   Submit OpenRequest to Broker
			//   Wait for broker to fill the order
			//   Create an open position from the filled order
			//   Account Update Balace, Margin and Equity
			//   Place Open Position in Account Portfolio
			//   Continue to next candle

		}
	}

	fmt.Println("End of data need to close any open requests")
	// Finished
	// If candles are used up
	//   Close out any open position
	//		Do everything in the Decision above
	//   Update Account with PnL, Balance
	//   Generate Backtest Report

	return nil
}
