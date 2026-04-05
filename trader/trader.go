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

	currentAccount *account.Account
}

type ConfigBackTest struct {
	Instrument string
	Account    string
	types.TimeRange
}

func (t *Trader) BackTest(ctx context.Context, cfg ConfigBackTest) error {

	// Select an Account
	t.currentAccount = t.AccountManager.Get(cfg.Account)
	if t.currentAccount == nil {
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

		err := t.currentAccount.ResolveWithMarks(map[string]types.Price{
			cfg.Instrument: candle.Close,
		})
		if err != nil {
			return err
		}

		plan := strategy.Update(ctx, &candle)
		for _, cancel := range plan.Cancel {
			// TODO find the Order that needs to be canceled and cancel it.
			fmt.Println("cancel: ", cancel)
		}

		for _, cl := range plan.Closes {

			fmt.Printf(" close: %v\n", cl)
			//   Submit CloseRequest to broker
			err = t.Broker.SubmitClose(ctx, cl)
			if err != nil {
				return err
			}
		}

		for _, op := range plan.Opens {
			op.ReqTimestamp = itr.Timestamp()

			fmt.Printf("  open: %v\n", op)

			// get sizing from Account
			err := t.currentAccount.SizePosition(op)
			if err != nil {
				return err
			}
			err = t.Broker.SubmitOpen(ctx, op)
			if err != nil {
				return err
			}
		}
	}

	fmt.Println("End of data need to close any open requests")
	fmt.Printf("Account positions %d - trades %d\n", t.currentAccount.Positions.Len(), t.currentAccount.Trades.Len())
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
		pos := evt.Position
		if pos == nil {
			err := fmt.Errorf("error order filled with no position")
			return err
		}

		err := t.currentAccount.AddPosition(ctx, pos)
		if err != nil {
			panic(err)
		}

		// TODO Journal the new position

	case broker.EventPositionClosed:
		// We have the close event from the broker

		fmt.Printf("ORDER Closed: %v\n", evt)
		pos := evt.Position
		trade := evt.Trade
		panic(pos == nil) // should always have position
		panic(trade == nil)

		// Delete position from Account portfolio, and adds trade
		err := t.currentAccount.ClosePosition(pos, trade)
		if err != nil {
			panic(err)
		}

		// TODO Journal the closed position, trade and account

	default:
		fmt.Printf("Either unknown or unsupported event: %v\n", evt.Type)

	}
	return nil
}
