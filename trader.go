package trader

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Trader struct {
	*Account
	*DataManager
	*TradeBook
	*Broker
}

type ConfigBackTest struct {
	Instrument string
	Strategy   string
	TimeFrame  Timeframe
	Start      time.Time
	End        time.Time

	Account string
}

type backtestStrategy interface {
	Name() string
	Update(context.Context, *CandleTime, *Positions) *StrategyPlan
}

type noopBacktestStrategy struct {
	NoopStrategy
}

func (n noopBacktestStrategy) Update(ctx context.Context, c *CandleTime, _ *Positions) *StrategyPlan {
	if c == nil {
		return n.NoopStrategy.Update(ctx, nil)
	}
	candle := c.Candle
	return n.NoopStrategy.Update(ctx, &candle)
}

func resolveBacktestStrategy(cfg *ConfigBackTest) (backtestStrategy, error) {
	strategyName := strings.ToLower(strings.TrimSpace(cfg.Strategy))
	switch strategyName {
	case "", "fake":
		return &Fake{
			StrategyBaseConfig: StrategyBaseConfig{
				Instrument: cfg.Instrument,
			},
			CandleCount: 10,
		}, nil
	case "noop", "no-op":
		return noopBacktestStrategy{NoopStrategy: NoopStrategy{}}, nil
	default:
		return nil, fmt.Errorf("unsupported strategy %q", cfg.Strategy)
	}
}

func (t *Trader) startBrokerEventHandler(ctx context.Context, evtQ <-chan *Event) (<-chan error, <-chan struct{}) {
	errCh := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-evtQ:
				if !ok {
					Backtest.Info("broker event channel closed")
					return
				}
				if err := t.processEvent(ctx, evt); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}
	}()

	return errCh, done
}

func (t *Trader) brokerEventError(errCh <-chan error) error {
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (t *Trader) backTestWithIterator(ctx context.Context, cfg *ConfigBackTest, strategy backtestStrategy, itr CandleIterator) (err error) {
	if itr == nil {
		return fmt.Errorf("nil candle iterator")
	}

	defer func() {
		closeErr := itr.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	evtQ := t.Broker.Events()
	Backtest.Debug("broker event handler started")
	errCh, done := t.startBrokerEventHandler(runCtx, evtQ)
	defer func() {
		cancel()
		<-done
		if err == nil {
			err = t.brokerEventError(errCh)
		}
	}()

	processedCandles := 0
	submittedOpens := 0
	submittedCloses := 0

	for itr.Next() {
		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := t.brokerEventError(errCh); err != nil {
			return err
		}

		candle := itr.CandleTime()
		Backtest.Debug("candle", "candle", processedCandles, "candle", candle.Candle.String())
		processedCandles++

		err := t.Account.ResolveWithMarks(map[string]Price{
			cfg.Instrument: candle.Close,
		})
		if err != nil {
			return err
		}

		plan := strategy.Update(runCtx, &candle, &t.Account.Positions)
		Backtest.Debug("strategy.Update plan", "open", len(plan.Opens), "closes", len(plan.Closes), "cancel", len(plan.Cancel))

		for _, cancelReq := range plan.Cancel {
			Backtest.Warn("TODO - cancel request not implemented", "cancel", cancelReq)
		}
		for _, cl := range plan.Closes {
			Backtest.Info("submit close request", "ID", cl.Request.ID)

			err = t.Broker.SubmitClose(runCtx, cl)
			if err != nil {
				return err
			}
			submittedCloses++
		}

		for _, openReq := range plan.Opens {
			Backtest.Info("Broker event Open Position", "ID", openReq.ID)
			err := t.Account.SizePosition(openReq)
			if err != nil {
				return err
			}

			Backtest.Info("Open position size", "ID", openReq.ID, "size", openReq.Units)
			_, err = t.Broker.OpenRequest(runCtx, openReq)
			if err != nil {
				return err
			}
			submittedOpens++
		}
	}

	if err := itr.Err(); err != nil {
		return err
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	if err := t.brokerEventError(errCh); err != nil {
		return err
	}

	Backtest.Info("backtest finished", "candles", processedCandles, "opens", submittedOpens, "closes", submittedCloses, "positions", t.Account.Positions.Len(), "trades", len(t.Account.Trades))

	return nil
}

func (t *Trader) BackTest(ctx context.Context, cfg *ConfigBackTest) error {
	Backtest.Info("backtest start", "instrument", cfg.Instrument, "account", cfg.Account)
	if t == nil {
		return fmt.Errorf("nil trader")
	}
	if cfg == nil {
		return fmt.Errorf("nil backtest config")
	}
	if t.Account == nil {
		return fmt.Errorf("nil account")
	}
	if t.Broker == nil {
		return fmt.Errorf("nil broker")
	}
	if t.DataManager == nil {
		return fmt.Errorf("nil data manager")
	}

	strategy, err := resolveBacktestStrategy(cfg)
	if err != nil {
		return err
	}
	Backtest.Info("strategy selected", "strategy", strategy.Name())

	// Select the Instrument, TimeRange and TimeFrame
	candlereq := CandleRequest{
		Source:     "candles",
		Instrument: cfg.Instrument,
		Range:      NewTimeRange(FromTime(cfg.Start), FromTime(cfg.End), cfg.TimeFrame),
	}
	Backtest.Debug("candle request prepared", "source", candlereq.Source, "instrument", candlereq.Instrument, "timeframe", candlereq.Range.TF)

	// Grab the candle iterator for this backtest
	itr, err := t.DataManager.Candles(ctx, candlereq)
	if err != nil {
		return err
	}

	// Finished
	// If candles are used up
	//   Close out any open position
	//		Do everything in the Decision above
	//   Update Account with PnL, Balance
	//   Generate Backtest Report

	return t.backTestWithIterator(ctx, cfg, strategy, itr)
}

func (t *Trader) processEvent(ctx context.Context, evt *Event) error {
	if evt == nil {
		return fmt.Errorf("nil broker event")
	}

	Backtest.Info("broker event recieved",
		"type", evt.Type.String(),
		"clientOrder", evt.ClientOrderID,
		"brokerOrder", evt.BrokerOrderID,
		"positionID", evt.PositionID,
		"reason", evt.Reason,
		"cause", evt.Cause.String())

	switch evt.Type {
	case EventOrderFilled:
		pos := evt.Position
		if pos == nil {
			return fmt.Errorf("error order filled with no position")
		}

		if err := t.Account.AddPosition(ctx, pos); err != nil {
			return err
		}

	case EventPositionClosed:
		pos := evt.Position
		trade := evt.Trade
		if pos == nil {
			return fmt.Errorf("position closed event missing position")
		}
		if trade == nil {
			return fmt.Errorf("position closed event missing trade")
		}

		if err := t.Account.ClosePosition(pos, trade); err != nil {
			return err
		}

	default:
		Backtest.Warn("unsupported broker event", "eventType", evt.Type)
	}

	return nil
}
