package trader

import (
	"slices"

	remotestrat "github.com/rustyeddy/trader/strategy"
)

// feedbackEvent is one entry of an order's history, with the order it belongs to.
type feedbackEvent struct {
	order *order
	orderEvent
}

// feedbacker is implemented by a strategy that wants to be told what the trader
// did with its orders. The trader calls feedback once per frame, just before
// onBars, with the events that happened since the last call, in the order they
// happened. They are the broker's work on the frame about to be shown to the
// strategy and the outcome of the orders it asked for in the previous call; never
// the outcome of the orders it is about to return. A strategy that does not
// implement it is simply not told.
type feedbacker interface {
	feedback(events []feedbackEvent)
}

// feedbackLog remembers what has been told to a feedbacker, so that each event is
// delivered once.
type feedbackLog struct {
	watched   []*order       // the strategy's orders whose history is not all delivered
	sent      map[*order]int // how many history entries of each were delivered
	exitsSeen int            // how much of broker.completed was looked at for the broker's own exits
}

// watch starts following a strategy's order.
func (tr *trader) watch(o *order) {
	if _, ok := tr.strategy.(feedbacker); ok {
		tr.feedbackLog.watched = append(tr.feedbackLog.watched, o)
	}
}

// reported says whether a state is one the strategy is told about. Requested and
// sized are the trader's own steps on the way to accepted.
func reported(s orderState) bool {
	switch s {
	case stateSubmitted, stateFilled, stateRejected, stateCancelled:
		return true
	}
	return false
}

// collectEvents returns what the strategy has not yet been told, in the order it
// happened (by time; on a tie the strategy's own orders come before the broker's
// exits, which is how they happen on one bar), and marks it told. It reports:
//
//   - the submitted, filled, rejected and cancelled steps of the strategy's orders;
//   - the broker's own exits (protective stop, take profit, margin closeout) when
//     they fill. Their other steps are the broker's housekeeping and are not
//     reported.
func (tr *trader) collectEvents() []feedbackEvent {
	log := &tr.feedbackLog
	if log.sent == nil {
		log.sent = make(map[*order]int)
	}
	var events []feedbackEvent
	for _, o := range log.watched {
		for _, e := range o.history[log.sent[o]:] {
			if reported(e.state) {
				events = append(events, feedbackEvent{o, e})
			}
		}
		log.sent[o] = len(o.history)
	}
	log.watched = slices.DeleteFunc(log.watched, func(o *order) bool {
		done := o.state == stateFilled || o.state == stateRejected || o.state == stateCancelled
		if done {
			delete(log.sent, o)
		}
		return done
	})

	for _, o := range tr.broker.completed[log.exitsSeen:] {
		if o.accountID == tr.account.id && o.reason != reasonStrategy && o.state == stateFilled {
			events = append(events, feedbackEvent{o, o.history[len(o.history)-1]})
		}
	}
	log.exitsSeen = len(tr.broker.completed)

	slices.SortStableFunc(events, func(a, b feedbackEvent) int { return a.time.Compare(b.time) })
	return events
}

// toWireEvent maps an event onto the public form. ref is the worker's label for
// the order, empty for the broker's own orders.
func toWireEvent(e feedbackEvent, ref string) remotestrat.Event {
	o := e.order
	w := remotestrat.Event{
		Time:       e.time,
		ID:         o.id,
		Ref:        ref,
		Instrument: o.instrument,
		Action:     o.action.String(),
		Units:      o.units,
		Cause:      o.reason.String(),
	}
	if o.direction != noDirection {
		w.Direction = o.direction.String()
	}
	switch e.state {
	case stateSubmitted:
		w.Kind = remotestrat.Accepted
	case stateFilled:
		w.Kind = remotestrat.Filled
		w.Price = o.fillPrice
	case stateRejected:
		w.Kind = remotestrat.Rejected
		w.Reason = e.reason
	case stateCancelled:
		w.Kind = remotestrat.Cancelled
		w.Reason = e.reason
	}
	return w
}
