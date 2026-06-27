package backtest

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rustyeddy/trader/engine"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/planner"
	"github.com/rustyeddy/trader/strategy"
)

func (run *Backtest) runWithIterator(ctx context.Context, t *engine.Trader, itr market.CandleIterator) (err error) {
	if itr == nil {
		return fmt.Errorf("nil candle iterator")
	}

	strat := run.Request.Strategy
	if strat == nil {
		return fmt.Errorf("nil strategy")
	}
	if run.State == nil {
		run.State = &BacktestRun{}
	}
	strat.Reset()

	exit := run.Request.Exit
	if exit == nil {
		exit = strategy.NoopExit{}
	}

	regime := run.Request.Regime
	if regime == nil {
		regime = strategy.NoopRegime{}
	}

	// Convert slippage and max-spread pips to Price units using instrument metadata.
	var slippage, maxSpread market.Price
	if inst := market.GetInstrument(run.Request.Instrument); inst != nil {
		if run.Request.SlippagePips != 0 {
			slippage = inst.PriceDeltaFromPips(run.Request.SlippagePips)
		}
		if run.Request.MaxSpreadPips != 0 {
			maxSpread = inst.PriceDeltaFromPips(run.Request.MaxSpreadPips)
		}
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

	var processedEvents int64
	errCh, done := t.StartBrokerEventHandler(runCtx, evtQ, &processedEvents)
	defer func() {
		// Give the broker event handler a short chance to drain queued events
		// before cancellation, otherwise late errors can be dropped.
		drainUntil := time.Now().Add(2 * time.Second)
		for time.Now().Before(drainUntil) {
			if err == nil {
				if evtErr := t.BrokerEventError(errCh); evtErr != nil {
					err = evtErr
					break
				}
			}

			pending := 0
			if t != nil {
				pending = t.Broker.EventQueueLen()
			}
			if pending == 0 {
				break
			}
			log.L.Debug("backtest drain events", "events", pending)
			time.Sleep(1 * time.Millisecond)
		}

		cancel()
		<-done
		if err == nil {
			err = t.BrokerEventError(errCh)
		}
	}()

	var processedCandles int64
	var submittedOpens int64
	var submittedCloses int64
	var lastCandle market.CandleTime
	haveLastCandle := false

	var lastProgressNanos int64
	atomic.StoreInt64(&lastProgressNanos, time.Now().UnixNano())

	watchdogDone := make(chan struct{})
	defer close(watchdogDone)

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-watchdogDone:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				candles := atomic.LoadInt64(&processedCandles)
				events := atomic.LoadInt64(&processedEvents)
				opens := atomic.LoadInt64(&submittedOpens)
				closes := atomic.LoadInt64(&submittedCloses)
				lag := time.Since(time.Unix(0, atomic.LoadInt64(&lastProgressNanos)))

				queueLen := 0
				queueCap := 0
				if t != nil {
					queueLen = t.Broker.EventQueueLen()
					queueCap = t.Broker.EventQueueCap()
				}

				if lag > 30*time.Second {
					log.L.Warn("watchdog: backtest appears stalled",
						"noProgressFor", lag.String(),
						"candles", candles,
						"events", events,
						"opens", opens,
						"closes", closes,
						"evtQueueLen", queueLen,
						"evtQueueCap", queueCap,
					)
					continue
				}

				log.L.Debug("watchdog: progress",
					"candles", candles,
					"events", events,
					"opens", opens,
					"closes", closes,
					"evtQueueLen", queueLen,
					"evtQueueCap", queueCap,
				)
			}
		}
	}()

	var pl planner.DefaultPlanner

	for {
		atomic.StoreInt64(&lastProgressNanos, time.Now().UnixNano())
		candle, ok := itr.Next()
		if !ok {
			break
		}
		atomic.StoreInt64(&lastProgressNanos, time.Now().UnixNano())

		if err := runCtx.Err(); err != nil {
			return err
		}
		if err := t.BrokerEventError(errCh); err != nil {
			return err
		}

		lastCandle = candle
		haveLastCandle = true
		// backtest.Debug("candle", "candle", processedCandles, "candle", candle.Candle.String())
		atomic.AddInt64(&processedCandles, 1)

		err := t.Account.ResolveWithMarks(map[string]market.Price{
			run.Request.Instrument: candle.Close,
		})
		if err != nil {
			return err
		}

		// Tick regime filter and exit strategy indicators every bar.
		regime.Tick(candle)
		exit.Tick(candle.Candle)

		// Update trailing/chandelier stops on all open lots.
		if exit.Ready() {
			_ = t.Account.Lots.Range(func(lot *execution.Lot) error {
				if lot == nil || lot.State != execution.LotOpen {
					return nil
				}
				// Advance extreme price watermark.
				switch lot.Side {
				case market.Long:
					if lot.ExtremePrice == 0 || candle.High > lot.ExtremePrice {
						lot.ExtremePrice = candle.High
					}
				case market.Short:
					if lot.ExtremePrice == 0 || candle.Low < lot.ExtremePrice {
						lot.ExtremePrice = candle.Low
					}
				}
				lot.Stop = exit.UpdateStop(lot.Side, lot.Stop, lot.EntryPrice, lot.ExtremePrice, candle.Candle)
				return nil
			})
		}

		autoExits, err := autoCloseExits(runCtx, t.Broker, candle, slippage)
		if err != nil {
			return err
		}
		if autoExits > 0 {
			atomic.AddInt64(&submittedCloses, int64(autoExits))
		}

		lots := engine.SnapshotLots(&t.Account.Lots)
		run.State.Lots = lots
		sig := strat.Update(runCtx, &candle, run)

		// Finalize the strategy's signal into broker-ready requests: regime gate,
		// max-spread gate, fill-price adjustment, initial stop, and sizing all
		// live in the planner.
		pc := runPlanContext{
			instrument:      run.Request.Instrument,
			acct:            t.Account,
			exit:            exit,
			regime:          regime,
			candle:          candle,
			slippage:        slippage,
			maxSpread:       maxSpread,
			defaultStopPips: run.Request.DefaultStopPips,
		}
		var stats planner.Stats
		plan, stats, err := pl.PlanSignal(sig, pc)
		if err != nil {
			return err
		}
		run.State.SpreadFiltered += stats.SpreadFiltered
		run.State.SpreadOpened += stats.SpreadOpened
		run.State.SpreadSum += stats.SpreadSum

		for _, cl := range plan.Closes {
			log.L.Info("submit close request", "ID", cl.Request.ID)

			atomic.StoreInt64(&lastProgressNanos, time.Now().UnixNano())
			err = t.Broker.SubmitClose(runCtx, cl)
			if err != nil {
				return err
			}
			if cl.Lot != nil {
				cl.Lot.State = execution.LotCloseRequested
			}
			atomic.AddInt64(&submittedCloses, 1)
		}

		for _, openReq := range plan.Opens {
			log.L.Info("Broker event Open Position", "ID", openReq.ID)
			log.L.Info("Open position size", "ID", openReq.ID, "size", openReq.Units)
			atomic.StoreInt64(&lastProgressNanos, time.Now().UnixNano())
			_, err = t.Broker.SubmitOpen(runCtx, openReq)
			if err != nil {
				t.Account.Lots.Delete(openReq.ID)
				return err
			}
			atomic.AddInt64(&submittedOpens, 1)
		}
	}

	if err := itr.Err(); err != nil {
		return err
	}
	if err := t.WaitForBrokerIdle(errCh, 2*time.Second); err != nil {
		return err
	}
	if haveLastCandle {
		var remaining []*execution.Lot
		_ = t.Account.Lots.Range(func(lot *execution.Lot) error {
			if lot != nil && lot.State == execution.LotOpen {
				remaining = append(remaining, lot)
			}
			return nil
		})

		for _, lot := range remaining {
			isBuy := lot.Side == market.Short
			closePx := lastCandle.Close + execution.FillAdjust(isBuy, lastCandle.AvgSpread, slippage)
			cl := &execution.CloseRequest{
				Request: execution.Request{
					TradeCommon: lot.TradeCommon,
					Reason:      "end-of-backtest",
					RequestType: execution.RequestClose,
					Price:       closePx,
					Timestamp:   lastCandle.Timestamp,
				},
				Lot:        lot,
				CloseCause: execution.CloseManual,
			}

			if err := t.Broker.SubmitClose(runCtx, cl); err != nil {
				return err
			}
		}
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	if err := t.BrokerEventError(errCh); err != nil {
		return err
	}

	if err := t.WaitForBrokerIdle(errCh, 2*time.Second); err != nil {
		return err
	}

	log.L.Info("backtest finished", "candles", atomic.LoadInt64(&processedCandles),
		"events", atomic.LoadInt64(&processedEvents),
		"opens", atomic.LoadInt64(&submittedOpens),
		"closes", atomic.LoadInt64(&submittedCloses),
		"positions", t.Account.Lots.Len(),
		"trades", len(t.Account.Trades))

	return nil
}

// Execute runs the backtest end-to-end against the trader: it builds the candle
// iterator from the request, drives the run loop, and snapshots the result.
func (run *Backtest) Execute(ctx context.Context, t *engine.Trader) error {
	if run == nil || run.Request == nil || run.Request.Strategy == nil {
		return fmt.Errorf("nil backtest run")
	}

	log.L.Info("backtest start",
		"instrument", run.Request.Instrument,
		"strategy", run.Request.Strategy.Name(),
		"balance", run.Request.StartingBalance.Float64(),
		"timerange", run.Request.TimeRange.String(),
	)
	if t == nil {
		return fmt.Errorf("nil trader")
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
	source := firstNonEmpty(run.Request.Source, market.SourceOanda)
	// Select the Instrument, TimeRange and TimeFrame
	candlereq := marketdata.CandleRequest{
		Source:     source,
		Instrument: run.Request.Instrument,
		Range:      run.Request.TimeRange,
	}
	log.L.Debug("candle request prepared", "source", candlereq.Source, "instrument", candlereq.Instrument, "timeframe", candlereq.Range.TF)

	// Grab the candle iterator for this backtest
	itr, err := t.DataManager.Candles(ctx, candlereq)
	if err != nil {
		return err
	}

	run.Result = nil
	if err := run.runWithIterator(ctx, t, itr); err != nil {
		return err
	}
	if run.Result == nil {
		run.BuildBacktestResult(t.Account)
	}

	return nil
}
