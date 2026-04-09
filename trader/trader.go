package trader

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/data"
	tlog "github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/strategies"
	"github.com/rustyeddy/trader/types"
)

var l = tlog.Backtest

type Trader struct {
	*account.Account
	*data.DataManager
	*portfolio.TradeBook
	*broker.Broker
}

type ConfigBackTest struct {
	Instrument string
	Strategy   string
	TimeFrame  types.Timeframe
	Start      time.Time
	End        time.Time

	Account string
}

func (t *Trader) BackTest(ctx context.Context, cfg *ConfigBackTest) error {
	l.Info("backtest start", "instrument", cfg.Instrument, "account", cfg.Account)

	// Start up the broker event handler
	evtQ := t.Broker.Events()

	l.Debug("broker event handler started")
	go func() {
		// TODO Check broker events to see if any broker activity has
		// triggered, order fill, stop hit, etc.
		for {
			select {
			case evt, ok := <-evtQ:
				// make sure the channel has not closed
				if !ok {
					l.Info("broker event channel closed")
					// channel has been shutdown, we have no choice but to
					// just return
					return
				}
				err := t.processEvent(ctx, evt)
				if err != nil {
					l.Error("failed to process broker event", "eventType", evt.Type, "error", err)
				}
			}
		}
	}()

	// TODO have the strategy manager look it up and return
	strategy := strategies.Fake{
		StrategyConfig: strategies.StrategyConfig{
			Instrument: cfg.Instrument,
		},
		CandleCount: 10,
	}
	l.Info("strategy selected", "strategy", strategy.Name())

	// Select the Instrument, TimeRange and TimeFrame
	candlereq := data.CandleRequest{
		Source:     "candles",
		Instrument: cfg.Instrument,
		Range:      types.NewTimeRange(types.FromTime(cfg.Start), types.FromTime(cfg.End), types.M1),
	}
	l.Debug("candle request prepared", "source", candlereq.Source, "instrument", candlereq.Instrument, "timeframe", candlereq.Range.TF)

	// Grab the candle iterator for this backtest
	itr, err := t.DataManager.Candles(context.TODO(), candlereq)
	if err != nil {
		return err
	}
	processedCandles := 0
	submittedOpens := 0
	submittedCloses := 0

	for itr.Next() {
		candle := itr.CandleTime()
		l.Debug("candle", "candle", processedCandles, "candle", candle.Candle.String())
		processedCandles++

		// this should probaby be event driven not once a polling cycle
		err := t.Account.ResolveWithMarks(map[string]types.Price{
			cfg.Instrument: candle.Close,
		})
		if err != nil {
			return err
		}

		// TODO We can just add positions to the context.
		plan := strategy.Update(ctx, &candle, &t.Account.Positions)
		l.Debug("strategy.Update plan", "open", len(plan.Opens), "closes", len(plan.Closes), "cancel", len(plan.Cancel))

		for _, cancel := range plan.Cancel {
			// TODO find the Order that needs to be canceled and cancel it.
			l.Warn("TODO - cancel request not implemented", "cancel", cancel)
		}
		for _, cl := range plan.Closes {
			fmt.Printf("CL: %+v\n", cl)
			l.Info("submit close request", "ID", cl.ID)

			// TODO: sanitize the close request
			err = t.Broker.SubmitClose(ctx, cl)
			if err != nil {
				return err
			}
		}

		for _, openReq := range plan.Opens {
			l.Info("Broker event Open Position", "ID", openReq.ID)
			err := t.Account.SizePosition(openReq)
			if err != nil {
				return err
			}

			l.Info("Open position size", "ID", openReq.ID, "size", openReq.Units)
			err = t.Broker.OpenRequest(ctx, openReq)
			if err != nil {
				return err
			}
		}
	}

	l.Info("backtest finished", "candles", processedCandles, "opens", submittedOpens, "closes", submittedCloses, "positions", t.Account.Positions.Len(), "trades", len(t.Account.Trades))
	// Finished
	// If candles are used up
	//   Close out any open position
	//		Do everything in the Decision above
	//   Update Account with PnL, Balance
	//   Generate Backtest Report

	return nil
}

func breakpoint() {}

func (t *Trader) processEvent(ctx context.Context, evt *broker.Event) error {

	l.Info("broker event recieved",
		"type", evt.Type.String(),
		"clientOrder", evt.ClientOrderID,
		"brokerOrder", evt.BrokerOrderID,
		"positionID", evt.PositionID,
		"reason", evt.Reason,
		"cause", evt.Cause.String())

	switch evt.Type {
	case broker.EventOrderFilled:
		pos := evt.Position
		if pos == nil {
			err := fmt.Errorf("error order filled with no position")
			return err
		}

		err := t.Account.AddPosition(ctx, pos)
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
		err := t.Account.ClosePosition(pos, trade)
		if err != nil {
			panic(err)
		}

		// TODO Journal the closed position, trade and account

	default:
		fmt.Printf("Either unknown or unsupported event: %v\n", evt.Type)

	}
	return nil
}
