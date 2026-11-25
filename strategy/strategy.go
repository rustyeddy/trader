// Package strategy carries a run between Trader, a long-lived server, and a
// strategy running in another process on this machine. It is the transport only:
// it knows nothing of orders, accounts or the engine, and it must not import the
// trader package.
//
// The strategy is the client. It connects to Trader's strategy address, asks for
// one backtest, trades it and receives the result (Run); Trader accepts one such
// connection at a time (ServeRuns). Every message is JSON, preceded by its length
// as four big-endian bytes, and a field the protocol does not define is an error.
//
// The client opens with the run it wants, the only thing it chooses (the account,
// sizing and risk are Trader's):
//
//	{"run":{"instruments":["EURUSD"],"interval":"h1",
//	        "from":"2024-01-01T00:00:00Z","to":"2024-04-01T00:00:00Z"}}
//
// and Trader answers with the RunID once the run exists, or with why it will not
// start one:
//
//	{"accepted":{"run_id":"run_01KB5Q3V7T2N4XG9YB6HFA1V7Z"}}
//	{"error":"interval \"m1\" is not supported"}
//
// Then, for every group of bars, Trader sends
//
//	{"bars":[...],"events":[...]}
//
// (marketdata.Bar as encoding/json writes it; the events are what the trader did
// since the last group, see Event, and may be empty) and the strategy answers
//
//	{"orders":[{"instrument":"EURUSD","action":"open","direction":"long",
//	            "price":"1.10000","stop_loss":"1.09000"}]}
//
// Before the first group Trader sends the bars that precede the range it trades,
// each group marked "warmup":true, with no events. They are for the strategy to
// prefill its indicators from; it answers {"orders":[]}, and an answer with an
// order is an error, since nothing is traded on them. A strategy that does not use
// history ignores them (see WithWarmup).
//
// The orders list must be present, but may be empty; an answer without it, or
// with null, is an error. Each order is a request (see Order): the trader decides
// the size, whether to admit it, and everything that follows. A field the protocol
// does not define, such as units, is an error, not ignored.
//
// The run ends with a last message from Trader, after which it closes the
// connection (see Result):
//
//	{"result":{"run_id":"run_...","status":"completed","summary":{...}}}
//
// The summary is Trader's, passed through as JSON. Its "settings" object says what
// the engine sized and admitted the run's orders under (read-only, never part of a
// request): risk_per_trade, max_order_notional, margin_closeout_level (absent for a
// custom margin policy) and instruments, a list of instrument, margin_rate,
// min_units, unit_step and max_units (absent when unlimited). The starting balance
// is the summary's initial_balance.
//
// Both sides are loopback-only: nothing here is authenticated or encrypted.
package strategy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// barsMessage is what Trader sends the strategy after the run is accepted: a
// group of bars, or, as the last message, the result. A result message has no
// bars and no events.
type barsMessage struct {
	Bars   []marketdata.Bar `json:"bars"`
	Events []Event          `json:"events"`
	// Warmup marks bars from before the range being traded: the strategy learns from
	// them (fills its indicators) and answers with no orders.
	Warmup bool    `json:"warmup,omitempty"`
	Result *Result `json:"result,omitempty"`
}

// resultMessage is the last message of a run.
type resultMessage struct {
	Result Result `json:"result"`
}

// runMessage is the first message of a run.
type runMessage struct {
	Run RunRequest `json:"run"`
}

// startMessage is Trader's answer to the run request: the run exists, or it will
// not.
type startMessage struct {
	Accepted *acceptedRun `json:"accepted,omitempty"`
	Error    string       `json:"error,omitempty"`
}

// acceptedRun says the run exists. Trader sends it as soon as the run's journal does,
// before any bars, so a run that later fails or is cancelled still has its RunID.
type acceptedRun struct {
	RunID string `json:"run_id"`
}

// RunRequest is the backtest a strategy asks for: the instruments, the interval
// (the lower-case token, h1 or d1) and the range [From, To) to trade. Trader loads
// the bars, with the month before From as warm-up, exactly as for `trader
// backtest`. Nothing else is chosen by the strategy.
type RunRequest struct {
	Instruments []string  `json:"instruments"`
	Interval    string    `json:"interval"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
}

// Validate reports a request that cannot describe a run, so Trader can refuse it
// before it makes one. Whether the instruments exist and have data is Trader's to
// find out when it loads them.
func (r RunRequest) Validate() error {
	if len(r.Instruments) == 0 {
		return errors.New("no instruments")
	}
	for _, in := range r.Instruments {
		if in == "" {
			return errors.New("an instrument is empty")
		}
	}
	if r.Interval == "" {
		return errors.New("no interval")
	}
	if r.From.IsZero() || r.To.IsZero() {
		return errors.New("from and to are required")
	}
	if !r.From.Before(r.To) {
		return fmt.Errorf("from %s is not before to %s", r.From.Format(time.RFC3339), r.To.Format(time.RFC3339))
	}
	return nil
}

// Run statuses, as Trader records them in the run's run.json.
const (
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Result is how a run ended. The summary is Trader's own backtest summary as JSON,
// left undecoded so this package does not depend on the engine; Error says why a
// failed or cancelled run ended, and Summary is empty then.
type Result struct {
	RunID   string          `json:"run_id"`
	Status  string          `json:"status"`
	Error   string          `json:"error,omitempty"`
	Summary json.RawMessage `json:"summary,omitempty"`
}

type ordersMessage struct {
	Orders []Order `json:"orders"`
}

// Order is what a worker may ask for. It is a request on the wire, not the
// trader's order: the trader maps it onto its own order, sizes it, applies the
// account's risk rules and the broker executes it. A worker cannot say how many
// units, nor name an id, account, state or fill; such a field is refused.
//
// Prices are decimal strings. Action and Direction use the trader's own words.
type Order struct {
	Instrument string    `json:"instrument"`
	Action     string    `json:"action"`              // Action is "open", "add", "close" or "reduce".
	Direction  string    `json:"direction,omitempty"` // Direction is "long" or "short", for an open only.
	Type       string    `json:"type,omitempty"`      // Type is "market" (the default when empty), "limit" or "stop".
	Price      num.Price `json:"price,omitzero"`      // the reference price of a market order, or the trigger of a limit or stop order.
	StopLoss   num.Price `json:"stop_loss,omitzero"`  // StopLoss is where the protective stop goes; an open requires it.
	TakeProfit num.Price `json:"take_profit,omitzero"`
	// Ref is the worker's own label for the order. The trader echoes it on the
	// order's events so the worker can tell which request an event is about. It
	// means nothing to the trader.
	Ref string `json:"ref,omitempty"`
}

// Event kinds.
const (
	Accepted  = "accepted"  // the trader sized the order and queued it with the broker
	Filled    = "filled"    // the broker executed it
	Rejected  = "rejected"  // the trader or broker refused it; Reason says why
	Cancelled = "cancelled" // it was accepted, then withdrawn before it filled
)

// Event is something the trader did, reported to the worker with the next group
// of bars. It is a read-only fact: the worker cannot change it, and it carries no
// account or position state. Each event is delivered once, in the order it
// happened, and never before the trader knew it: an order the worker requests in
// one answer is first reported with the group after it.
type Event struct {
	// Time is when it happened: the opening time of the bar it happened on.
	Time time.Time `json:"time"`
	// Kind is one of Accepted, Filled, Rejected, Cancelled.
	Kind string `json:"kind"`
	// ID is the order's id, assigned by the trader. It is empty for an order
	// refused before it was queued, which never gets one.
	ID string `json:"id,omitempty"`
	// Ref is the Ref the worker gave the order; empty for an order the trader's
	// broker made itself, such as a protective stop.
	Ref        string `json:"ref,omitempty"`
	Instrument string `json:"instrument"`
	Action     string `json:"action"`
	Direction  string `json:"direction,omitempty"`
	// Units is the size the trader chose, for Accepted and Filled.
	Units num.Quantity `json:"units,omitzero"`
	// Price is the fill price, for Filled.
	Price num.Price `json:"price,omitzero"`
	// Cause is why the order exists: "strategy" for a worker's order, or
	// "stop loss", "take profit" or "margin closeout" for one the broker made. A
	// Filled event with cause "stop loss" is a protective-stop exit.
	Cause string `json:"cause"`
	// Reason says why, for Rejected.
	Reason string `json:"reason,omitempty"`
}

// requireLoopback refuses an address that is not on this machine's loopback
// interface. A host name other than localhost is refused too, since it may
// resolve anywhere.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("address %q is not loopback", addr)
	}
	return nil
}
