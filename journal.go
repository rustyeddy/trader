package trader

import (
	"encoding/json"
	"io"
	"time"

	"github.com/rustyeddy/trader/num"
)

// journal records every transition of every order of a run, one JSON line each, in
// the order the transitions happen. It is not a second record of the run: order
// history is still the authority, and the journal only copies what order.record
// writes into it, at the moment it writes it, so the final frame's transitions are
// in it too, whether or not any strategy was ever told of them.
//
// A line carries simulated time only, never the wall clock, so two runs that trade
// the same way write the same lines. Prices and sizes are decimal strings.
//
// The engine opens no file: the journal writes to an io.Writer its caller gives it.
// order.record has no way to return an error and must not change what the engine
// does, so a journal that cannot write keeps the first error (err), stops writing
// and lets the run go on; the run asks for the error at safe points and ends with
// it.
type journal struct {
	w        io.Writer
	seq      int
	ordinals map[*order]int
	failed   error
}

func newJournal(w io.Writer) *journal {
	return &journal{w: w, ordinals: make(map[*order]int)}
}

// journalLine is one line of the journal. Everything but seq, time, order and
// state is left out when it has no value, so a line says only what is new: the
// first line of an order (requested) says what was asked, the others what the
// transition changed, and the fill also says what the order is.
type journalLine struct {
	Seq   int       `json:"seq"`
	Time  time.Time `json:"time"`         // the simulated step the transition happened on
	Order int       `json:"order"`        // 1, 2, ... in the order the journal first saw each order
	ID    string    `json:"id,omitempty"` // the broker's id; empty until the order is queued, and for ever for a refusal
	State string    `json:"state"`

	// What was asked, on the requested line.
	Instrument string    `json:"instrument,omitempty"`
	Action     string    `json:"action,omitempty"`
	Direction  string    `json:"direction,omitempty"`
	Type       string    `json:"type,omitempty"`
	Cause      string    `json:"cause,omitempty"` // strategy, stop loss, take profit, margin closeout
	Price      num.Price `json:"price,omitzero"`
	StopLoss   num.Price `json:"stop_loss,omitzero"`
	TakeProfit num.Price `json:"take_profit,omitzero"`
	Trail      num.Price `json:"trail,omitzero"`
	Expires    time.Time `json:"expires,omitzero"`

	// What happened.
	Units     num.Quantity `json:"units,omitzero"`
	FillPrice num.Price    `json:"fill_price,omitzero"`
	Reason    string       `json:"reason,omitempty"` // why, for a rejection, a cancellation or an amendment
}

// record writes the transition e, which order.record has just applied to o.
func (j *journal) record(o *order, e orderEvent) {
	if j.failed != nil {
		return
	}
	n, seen := j.ordinals[o]
	if !seen {
		n = len(j.ordinals) + 1
		j.ordinals[o] = n
	}
	j.seq++
	l := journalLine{Seq: j.seq, Time: e.time.UTC(), Order: n, ID: o.id, State: stateName(e.state), Reason: e.reason}
	if !seen || e.state == stateFilled {
		// What the order is, on its first line and again on the fill, so a fill line
		// can be read alone: which instrument, what it did and why (a protective
		// stop's fill says "stop loss").
		l.Instrument, l.Action, l.Cause = o.instrument, o.action.String(), o.reason.String()
		if o.direction != noDirection {
			l.Direction = o.direction.String()
		}
	}
	if !seen {
		l.Type = o.orderType.String()
		l.Price, l.StopLoss, l.TakeProfit, l.Trail, l.Expires = o.price, o.stopLoss, o.takeProfit, o.trail, o.expires
	}
	switch e.state {
	case stateSized, stateSubmitted, stateFilled, stateRejected, stateCancelled:
		l.Units = o.units
	}
	if e.state == stateSized && !o.claimedPrice.IsZero() {
		l.Price = o.price // the decision close it was sized from, not the price asked
	}
	if e.state == stateFilled {
		l.FillPrice = o.fillPrice
	}
	line, err := json.Marshal(l)
	if err == nil {
		line = append(line, '\n')
		var n int
		n, err = j.w.Write(line)
		if err == nil && n < len(line) {
			err = io.ErrShortWrite // a writer may write less and say nothing: a cut line is a failure
		}
	}
	if err != nil {
		j.failed = err
	}
}

// err is the first error the journal met, or nil.
func (j *journal) err() error {
	if j == nil {
		return nil
	}
	return j.failed
}

// stateName is the word for a state in the journal. They are the journal's
// vocabulary, fixed whatever the engine calls its states.
func stateName(s orderState) string {
	switch s {
	case stateRequested:
		return "requested"
	case stateSized:
		return "sized"
	case stateSubmitted:
		return "submitted"
	case stateFilled:
		return "filled"
	case stateRejected:
		return "rejected"
	case stateCancelled:
		return "cancelled"
	}
	return "unknown"
}
