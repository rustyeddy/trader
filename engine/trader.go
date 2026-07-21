// Package engine is the low-level backtest/live execution mechanism: the Trader
// type, which drives an account.Ledger and drains its event queue while
// tracking open lots. It is pure mechanism with no notion of a "run" — the
// higher-level backtest package orchestrates runs on top of it
// (backtest -> engine; engine never imports backtest).
package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/log"
)

// Trader couples a candle source with the ledger/account it drives. It owns
// the ledger event loop; backtest and live orchestration call its exported
// primitives to run a session.
type Trader struct {
	DataManager CandleSource
	*account.Ledger
}

// StartBrokerEventHandler launches the goroutine that drains the broker event
// queue, processing each event and reporting the first error on the returned
// error channel. The done channel closes when the handler exits.
func (t *Trader) StartBrokerEventHandler(ctx context.Context, evtQ <-chan *account.Event, processed *int64) (<-chan error, <-chan struct{}) {
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
					log.L.Info("broker event channel closed")
					return
				}

				log.L.Debug("Broker event received",
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

// BrokerEventError returns the first pending broker-event error without
// blocking, or nil if none is queued.
func (t *Trader) BrokerEventError(errCh <-chan error) error {
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// SnapshotLots returns a deep copy of the open/pending lots in src so callers
// can hand a stable view to a strategy without racing the broker.
func SnapshotLots(src *account.LotBook) *account.LotBook {
	out := &account.LotBook{}
	if src == nil {
		return out
	}
	_ = src.Range(func(lot *account.Lot) error {
		if lot != nil && (lot.State == account.LotOpen || lot.State == account.LotOpenRequested || lot.State == account.LotCloseRequested) {
			_ = out.Add(lot.Clone())
		}
		return nil
	})
	return out
}

// WaitForBrokerIdle blocks until the broker's event queue is empty and no lot
// is in a pending open/close state, or until timeout elapses.
func (t *Trader) WaitForBrokerIdle(errCh <-chan error, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := t.BrokerEventError(errCh); err != nil {
			return err
		}

		queueLen := 0
		if t != nil {
			queueLen = t.Ledger.EventQueueLen()
		}

		pendingState := false
		if t != nil && t.Account != nil {
			_ = t.Account.Lots.Range(func(lot *account.Lot) error {
				if lot.State == account.LotOpenRequested || lot.State == account.LotCloseRequested {
					pendingState = true
				}
				return nil
			})
		}

		if queueLen == 0 && !pendingState {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("broker did not become idle within %s (evtQueueLen=%d pendingState=%t)", timeout, queueLen, pendingState)
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func (t *Trader) processEvent(ctx context.Context, evt *account.Event) error {
	if evt == nil {
		return fmt.Errorf("nil broker event")
	}

	log.L.Info("broker event recieved",
		"type", evt.Type.String(),
		"positionID", eventPositionID(evt))

	switch evt.Type {
	case account.EventOrderFilled:
		lot := evt.Lot
		if lot == nil {
			return fmt.Errorf("error order filled with no position")
		}

	case account.EventPositionClosed:
		lot := evt.Lot
		trade := evt.Trade
		if lot == nil {
			return fmt.Errorf("position closed event missing position")
		}
		if trade == nil {
			return fmt.Errorf("position closed event missing trade")
		}

	default:
		log.L.Warn("unsupported broker event", "eventType", evt.Type)
	}

	return nil
}

func eventPositionID(evt *account.Event) string {
	if evt == nil || evt.Lot == nil {
		return ""
	}
	return evt.Lot.ID
}
