package trader

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

type Trader struct {
	DataManager CandleSource
	*Broker
	*Store
}

func (t *Trader) startBrokerEventHandler(ctx context.Context, evtQ <-chan *Event, processed *int64) (<-chan error, <-chan struct{}) {
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
					L.Info("broker event channel closed")
					return
				}

				L.Debug("Broker event received",
					"type", evt.Type.String(),
					"positionID", eventPositionID(evt),
				)

				if err := t.processEvent(ctx, evt); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				if processed != nil {
					atomic.AddInt64(processed, 1)
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

func snapshotLots(src *LotBook) *LotBook {
	out := &LotBook{}
	if src == nil {
		return out
	}
	_ = src.Range(func(lot *Lot) error {
		if lot != nil && (lot.State == LotOpen || lot.State == LotOpenRequested || lot.State == LotCloseRequested) {
			out.Add(lot)
		}
		return nil
	})
	return out
}

func (t *Trader) waitForBrokerIdle(errCh <-chan error, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := t.brokerEventError(errCh); err != nil {
			return err
		}

		queueLen := 0
		if t != nil && t.Broker != nil && t.Broker.evtQ != nil {
			queueLen = len(t.Broker.evtQ)
		}

		pendingState := false
		if t != nil && t.Account != nil {
			_ = t.Account.Lots.Range(func(lot *Lot) error {
				if lot.State == LotOpenRequested || lot.State == LotCloseRequested {
					pendingState = true
				}
				return nil
			})
		}

		if queueLen == 0 && !pendingState {
			return nil
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func (t *Trader) backTestWithIterator(ctx context.Context, run *Backtest, itr CandleIterator) (err error) {
	if itr == nil {
		return fmt.Errorf("nil candle iterator")
	}

	strategy := run.Request.Strategy
	if strategy == nil {
		return fmt.Errorf("nil strategy")
	}
	strategy.Reset()

	exit := run.Request.Exit
	if exit == nil {
		exit = NoopExit{}
	}

	regime := run.Request.Regime
	if regime == nil {
		regime = NoopRegime{}
	}

	// Convert slippage and max-spread pips to Price units using instrument metadata.
	var slippage, maxSpread Price
	if inst := GetInstrument(run.Request.Instrument); inst != nil {
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
	errCh, done := t.startBrokerEventHandler(runCtx, evtQ, &processedEvents)
	defer func() {
		// Give the broker event handler a short chance to drain queued events
		// before cancellation, otherwise late errors can be dropped.
		drainUntil := time.Now().Add(2 * time.Second)
		for time.Now().Before(drainUntil) {
			if err == nil {
				if evtErr := t.brokerEventError(errCh); evtErr != nil {
					err = evtErr
					break
				}
			}

			pending := 0
			if t != nil && t.Broker != nil && t.Broker.evtQ != nil {
				pending = len(t.Broker.evtQ)
			}
			if pending == 0 {
				break
			}
			L.Debug("backtest drain events", "events", pending)
			time.Sleep(1 * time.Millisecond)
		}

		cancel()
		<-done
		if err == nil {
			err = t.brokerEventError(errCh)
		}
	}()

	var processedCandles int64
	var submittedOpens int64
	var submittedCloses int64
	var lastCandle candleTime
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
				if t != nil && t.Broker != nil && t.Broker.evtQ != nil {
					queueLen = len(t.Broker.evtQ)
					queueCap = cap(t.Broker.evtQ)
				}

				if lag > 30*time.Second {
					L.Warn("watchdog: backtest appears stalled",
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

				L.Debug("watchdog: progress",
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
		if err := t.brokerEventError(errCh); err != nil {
			return err
		}

		lastCandle = candle
		haveLastCandle = true
		// backtest.Debug("candle", "candle", processedCandles, "candle", candle.Candle.String())
		atomic.AddInt64(&processedCandles, 1)

		err := t.Account.ResolveWithMarks(map[string]Price{
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
			_ = t.Account.Lots.Range(func(lot *Lot) error {
				if lot == nil || lot.State != LotOpen {
					return nil
				}
				// Advance extreme price watermark.
				switch lot.Side {
				case Long:
					if lot.ExtremePrice == 0 || candle.High > lot.ExtremePrice {
						lot.ExtremePrice = candle.High
					}
				case Short:
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

		strategyCtx := withStrategyRuntime(runCtx, run.Request.Instrument, int(processedCandles), 0, t.Account)
		lots := snapshotLots(&t.Account.Lots)
		run.State.Lots = lots
		plan := strategy.Update(strategyCtx, &candle, run)
		if plan == nil {
			plan = &DefaultStrategyPlan
		}

		// Regime filter: suppress new entries in ranging/consolidating markets.
		// Existing positions continue to be managed by the exit strategy.
		if regime.Ready() {
			if !regime.Trending() {
				plan.Opens = nil
			} else if len(plan.Opens) > 0 {
				filtered := plan.Opens[:0]
				for _, o := range plan.Opens {
					if regime.AllowSide(o.Side) {
						filtered = append(filtered, o)
					}
				}
				plan.Opens = filtered
			}
		}

		// Max-spread filter: skip entries when the bid-ask spread is too wide
		// (market opens, news events, low-liquidity periods).
		if maxSpread > 0 && candle.AvgSpread > maxSpread && len(plan.Opens) > 0 {
			run.State.SpreadFiltered++
			plan.Opens = nil
		}

		for _, cancelReq := range plan.Cancel {
			L.Warn("TODO - cancel request not implemented", "cancel", cancelReq)
		}
		for _, cl := range plan.Closes {
			L.Info("submit close request", "ID", cl.Request.ID)

			// Short closes by buying at ask; long closes by selling at bid.
			if cl.Lot != nil {
				isBuy := cl.Lot.Side == Short
				cl.Price += fillAdjust(isBuy, candle.AvgSpread, slippage)
			}

			atomic.StoreInt64(&lastProgressNanos, time.Now().UnixNano())
			err = t.Broker.SubmitClose(runCtx, cl)
			if err != nil {
				return err
			}
			if cl.Lot != nil {
				cl.Lot.State = LotCloseRequested
			}
			atomic.AddInt64(&submittedCloses, 1)
		}

		for _, openReq := range plan.Opens {
			L.Info("Broker event Open Position", "ID", openReq.ID)

			// Long buys at ask; short sells at bid.
			isBuy := openReq.Side == Long
			openReq.Price += fillAdjust(isBuy, candle.AvgSpread, slippage)
			run.State.SpreadOpened++
			run.State.SpreadSum += candle.AvgSpread

			// Let the exit strategy override the initial stop when configured.
			if exit.Ready() {
				if s := exit.InitialStop(openReq.Side, openReq.Price, candle.Candle); s != 0 {
					openReq.Stop = s
				}
			}

			if openReq.Units == 0 {
				err := t.Account.SizePosition(openReq)
				if err != nil {
					return err
				}
			}

			L.Info("Open position size", "ID", openReq.ID, "size", openReq.Units)
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
	if err := t.waitForBrokerIdle(errCh, 2*time.Second); err != nil {
		return err
	}
	if haveLastCandle {
		var remaining []*Lot
		_ = t.Account.Lots.Range(func(lot *Lot) error {
			if lot != nil && lot.State == LotOpen {
				remaining = append(remaining, lot)
			}
			return nil
		})

		for _, lot := range remaining {
			isBuy := lot.Side == Short
			closePx := lastCandle.Close + fillAdjust(isBuy, lastCandle.AvgSpread, slippage)
			cl := &CloseRequest{
				Request: Request{
					TradeCommon: lot.TradeCommon,
					Reason:      "end-of-backtest",
					RequestType: RequestClose,
					Price:       closePx,
					Timestamp:   lastCandle.Timestamp,
				},
				Lot:        lot,
				CloseCause: CloseManual,
			}

			if err := t.Broker.SubmitClose(runCtx, cl); err != nil {
				return err
			}
		}
	}
	if err := runCtx.Err(); err != nil {
		return err
	}
	if err := t.brokerEventError(errCh); err != nil {
		return err
	}

	if err := t.waitForBrokerIdle(errCh, 2*time.Second); err != nil {
		return err
	}

	L.Info("backtest finished", "candles", atomic.LoadInt64(&processedCandles),
		"events", atomic.LoadInt64(&processedEvents),
		"opens", atomic.LoadInt64(&submittedOpens),
		"closes", atomic.LoadInt64(&submittedCloses),
		"positions", t.Account.Lots.Len(),
		"trades", len(t.Account.Trades))

	return nil
}

func (t *Trader) Backtest(ctx context.Context, run *Backtest) error {
	if run == nil || run.Request == nil || run.Request.Strategy == nil {
		return fmt.Errorf("nil backtest run")
	}

	L.Info("backtest start",
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
	source := firstNonEmpty(run.Request.Source, SourceOanda)
	// Select the Instrument, TimeRange and TimeFrame
	candlereq := CandleRequest{
		Source:     source,
		Instrument: run.Request.Instrument,
		Range:      run.Request.TimeRange,
	}
	L.Debug("candle request prepared", "source", candlereq.Source, "instrument", candlereq.Instrument, "timeframe", candlereq.Range.TF)

	// Grab the candle iterator for this backtest
	itr, err := t.DataManager.Candles(ctx, candlereq)
	if err != nil {
		return err
	}

	run.Result = nil
	if err := t.backTestWithIterator(ctx, run, itr); err != nil {
		return err
	}
	if run.Result == nil {
		run.BuildBacktestResult(t.Account)
	}

	return nil
}

func (t *Trader) processEvent(ctx context.Context, evt *Event) error {
	if evt == nil {
		return fmt.Errorf("nil broker event")
	}

	L.Info("broker event recieved",
		"type", evt.Type.String(),
		"positionID", eventPositionID(evt))

	switch evt.Type {
	case EventOrderFilled:
		lot := evt.Lot
		if lot == nil {
			return fmt.Errorf("error order filled with no position")
		}

	case EventPositionClosed:
		lot := evt.Lot
		trade := evt.Trade
		if lot == nil {
			return fmt.Errorf("position closed event missing position")
		}
		if trade == nil {
			return fmt.Errorf("position closed event missing trade")
		}

	default:
		L.Warn("unsupported broker event", "eventType", evt.Type)
	}

	return nil
}

func eventPositionID(evt *Event) string {
	if evt == nil || evt.Lot == nil {
		return ""
	}
	return evt.Lot.ID
}
