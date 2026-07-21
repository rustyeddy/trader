package backtest

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/brokers/sim"
	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/engine"
	"github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/planner"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/types"
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
	var slippage, maxSpread types.Price
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

	evtQ := t.Account.Events()

	// brokerFills is Sim's own fill feed — obtained once, like evtQ, not
	// re-requested every bar (a real Broker's StreamTransactions opens a
	// long-lived connection; calling it repeatedly would be wrong once
	// live trading reuses this path). Drained once per bar, after feeding
	// the broker the bar's price — see the drainBrokerFills call below.
	var brokerFills <-chan oanda.TxEvent
	if t.Broker != nil {
		var streamErr error
		brokerFills, streamErr = t.Broker.StreamTransactions(runCtx, t.Account.ID, oanda.StreamOptions{})
		if streamErr != nil {
			return streamErr
		}
	}

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
				pending = t.Account.EventQueueLen()
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
					queueLen = t.Account.EventQueueLen()
					queueCap = t.Account.EventQueueCap()
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

		haveLastCandle = true
		// backtest.Debug("candle", "candle", processedCandles, "candle", candle.String())
		atomic.AddInt64(&processedCandles, 1)

		// Tick regime filter and exit strategy indicators every bar.
		regime.Tick(candle)
		exit.Tick(candle)

		// Update trailing/chandelier stops on all open lots.
		if exit.Ready() {
			_ = t.Account.Lots.Range(func(lot *account.Lot) error {
				if lot == nil || lot.State != account.LotOpen {
					return nil
				}
				// Advance extreme price watermark.
				switch lot.Side {
				case types.Long:
					if lot.ExtremePrice == 0 || candle.High > lot.ExtremePrice {
						lot.ExtremePrice = candle.High
					}
				case types.Short:
					if lot.ExtremePrice == 0 || candle.Low < lot.ExtremePrice {
						lot.ExtremePrice = candle.Low
					}
				}
				lot.Stop = exit.UpdateStop(lot.Side, lot.Stop, lot.EntryPrice, lot.ExtremePrice, candle)
				return nil
			})
		}

		// Feed the broker this bar's price (mark-to-market + resting
		// stop/take triggers, fused — see brokers/sim.Sim.UpdatePrice).
		// Must run after the trailing-stop update loop above so a
		// just-trailed stop is what gets checked, matching the relative
		// ordering autoCloseExits used to have with it. Only Sim (backtest
		// paper-trading) needs feeding this way; a real venue has its own
		// market data.
		if pu, ok := t.Broker.(brokers.PriceUpdater); ok {
			if err := pu.UpdatePrice(sim.TickFromCandle(run.Request.Instrument, candle)); err != nil {
				return err
			}
		}

		autoExits := drainBrokerFills(t.Account, brokerFills)
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

		if (len(plan.Closes) > 0 || len(plan.Opens) > 0) && t.Broker == nil {
			return fmt.Errorf("nil broker: cannot submit orders")
		}

		for _, cl := range plan.Closes {
			log.L.Info("submit close request", "ID", cl.Request.ID)

			atomic.StoreInt64(&lastProgressNanos, time.Now().UnixNano())
			if _, err = t.Broker.CloseTrade(runCtx, t.Account.ID, cl.Lot.ID, 0); err != nil {
				return err
			}
			if cl.Lot != nil {
				cl.Lot.State = account.LotCloseRequested
			}
			atomic.AddInt64(&submittedCloses, 1)
		}

		for _, openReq := range plan.Opens {
			log.L.Info("Broker event Open Position", "ID", openReq.ID)
			log.L.Info("Open position size", "ID", openReq.ID, "size", openReq.Units)
			atomic.StoreInt64(&lastProgressNanos, time.Now().UnixNano())

			signedUnits := int64(openReq.Units)
			if openReq.Side == types.Short {
				signedUnits = -signedUnits
			}
			res, err := t.Broker.SubmitMarketOrder(runCtx, t.Account.ID, openReq.Instrument, signedUnits, openReq.Stop.Float64())
			if err != nil {
				return err
			}
			// SubmitMarketOrder has no room for Reason/InitialStop (a real
			// broker order request doesn't carry app-specific analysis
			// metadata) — Account.SubmitOpen used to carry these for free
			// by cloning the whole OpenRequest.TradeCommon. Patch them
			// onto the fresh lot directly; Range gives the live pointer
			// (Lots.Get returns a clone, chunk 2's UpdateTradeStop bug).
			_ = t.Account.Lots.Range(func(lot *account.Lot) error {
				if lot.ID == res.TradeID {
					lot.Reason = openReq.Reason
					lot.InitialStop = openReq.InitialStop
				}
				return nil
			})
			atomic.AddInt64(&submittedOpens, 1)
		}
	}

	if err := itr.Err(); err != nil {
		return err
	}
	// Pick up this bar-loop's last opens/closes before checking idle —
	// drainBrokerFills only sees what's already on brokerFills, and the
	// per-bar drain above only catches fills through the *previous* bar's
	// submissions (this bar's opens/closes haven't been drained yet).
	drainBrokerFills(t.Account, brokerFills)
	if err := t.WaitForBrokerIdle(errCh, 2*time.Second); err != nil {
		return err
	}
	if haveLastCandle {
		var remaining []*account.Lot
		_ = t.Account.Lots.Range(func(lot *account.Lot) error {
			if lot != nil && lot.State == account.LotOpen {
				remaining = append(remaining, lot)
			}
			return nil
		})

		if len(remaining) > 0 && t.Broker == nil {
			return fmt.Errorf("nil broker: cannot submit orders")
		}
		for _, lot := range remaining {
			if _, err := t.Broker.CloseTrade(runCtx, t.Account.ID, lot.ID, 0); err != nil {
				return err
			}
		}
	}
	drainBrokerFills(t.Account, brokerFills)
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
	if t.DataManager == nil {
		return fmt.Errorf("nil data manager")
	}
	source := firstNonEmpty(run.Request.Source, market.SourceOanda)
	// Select the Instrument, TimeRange and TimeFrame
	candlereq := datamanager.CandleRequest{
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

// drainBrokerFills applies every fill currently queued on ch to acct's own
// event queue, translating oanda.TxEvent -> account.Event so
// engine.Trader's existing StartBrokerEventHandler/processEvent machinery
// sees it exactly as it would a synchronous SubmitOpen/SubmitClose call.
// Non-blocking — drains only what's already queued, not a blocking read —
// and safe to call with a nil ch (no Broker configured). Returns the
// number of close events applied, for the submittedCloses counter (a
// resting stop/take triggering counts as an auto-close, same accounting
// autoCloseExits used to report).
func drainBrokerFills(acct *account.Account, ch <-chan oanda.TxEvent) int {
	if ch == nil {
		return 0
	}
	closes := 0
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return closes
			}
			if evt.Err != nil {
				continue // stream error events aren't fills; nothing to apply
			}
			acct.EnqueueEvent(txEventToAccountEvent(acct, evt.Tx))
			if len(evt.Tx.TradesClosed) > 0 {
				closes++
			}
		default:
			return closes
		}
	}
}

// txEventToAccountEvent translates a Broker fill into the account.Event
// shape engine.Trader's processEvent expects.
func txEventToAccountEvent(acct *account.Account, tx oanda.Transaction) *account.Event {
	// Never look up the Lot via Account.Lots here — by the time a fill
	// gets drained, an open lot may already have been closed by a later
	// stop/take trigger (checkStopsAndTakes applies its close to Account
	// synchronously, well before this bridge ever runs), so Lots.Get would
	// race and intermittently return nil for a perfectly valid open event
	// (confirmed live: "error order filled with no position" on a
	// short-lived position). Always synthesize a minimal placeholder Lot
	// from the event's own fields instead — processEvent only nil-checks
	// it, never inspects its contents, for either event type.
	lot := &account.Lot{TradeCommon: &account.TradeCommon{ID: tx.TradeID, Instrument: tx.Instrument}}

	if len(tx.TradesClosed) == 0 {
		return &account.Event{Type: account.EventOrderFilled, Lot: lot}
	}

	var trade *account.Trade
	for _, tr := range acct.Trades {
		if tr.ID == tx.TradeID {
			trade = tr
			break
		}
	}
	return &account.Event{Type: account.EventPositionClosed, Lot: lot, Trade: trade}
}
