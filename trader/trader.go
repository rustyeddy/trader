package trader

import (
	"context"
	"fmt"
	"log"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/data"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/strategies"
	"github.com/rustyeddy/trader/types"
)

type Trader struct {
	*market.Instrument
	*account.AccountManager
	*data.DataManager
	broker.Broker
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

	// Start up the broker event handler
	evtQ := t.Broker.Events()
	go func() {
		// TODO Check broker events to see if any broker activity has
		// triggered, order fill, stop hit, etc.
		for {
			select {
			case evt, ok := <-evtQ:
				// make sure the channel has not closed
				if !ok {
					// channel has been shutdown, we have no choice but to
					// just return
					return
				}
				err := t.processEvent(ctx, evt)
				if err != nil {
					log.Println("Log this error: ", err)
				}
			}
		}
	}()

	// Grab the candle iterator for this backtest
	itr, err := t.DataManager.Candles(context.TODO(), candlereq)
	if err != nil {
		return err
	}

	for itr.Next() {
		candle := itr.Candle()

		err := account.ResolveWithMarks(map[string]types.Price{
			cfg.Instrument: candle.Close,
		})
		if err != nil {
			return err
		}

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
			op.ReqTimestamp = itr.Timestamp()

			// get sizing from Account
			err := account.SizePosition(op)
			if err != nil {
				return err
			}
			err = t.Broker.SubmitOpen(ctx, op)
			if err != nil {
				return err
			}

			// We are done, we will pickup the broker event
			// when the order is finally filled.

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

func (t *Trader) processEvent(ctx context.Context, evt *broker.Event) error {

	switch evt.Type {
	case broker.EventOrderFilled:
		fmt.Printf("ORDER Filled: %v\n", evt)

		// TODO
		//   Account Update Balace, Margin and Equity
		//   Place Open Position in Account Portfolio
		//   Continue to next candle

		pos := evt.Position
		if pos == nil {
			err := fmt.Errorf("error order filled with no position")
			return err
		}

	case broker.EventPositionClosed:
		fmt.Printf("ORDER Closed: %v\n", evt)

	default:
		fmt.Printf("Either unknown or unsupported event: %v\n", evt.Type)

	}
	return nil
}
