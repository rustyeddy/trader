package strategy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// Run is the strategy's side of one backtest. It connects to Trader's strategy
// address (a loopback address), asks for the run, and then answers every group of
// bars with onBars until Trader sends the result, which it returns. Cancelling ctx
// abandons the run and returns ctx's error; Trader fails that run and keeps its
// journal.
//
// The error is for a run that could not be had or followed: Trader unreachable or
// refusing the request (its reason is in the error), the connection lost before the
// result, a message that is not the protocol, or onBars failing (Run then hangs up
// and returns its error). A run that Trader finished as failed or cancelled is a
// Result with that Status, not an error.
//
// onBars is called with each group of bars and the events since the last group, and
// returns the orders to request, in order, possibly none.
func Run(ctx context.Context, addr string, req RunRequest, onBars func(bars []marketdata.Bar, events []Event) ([]Order, error), opts ...Option) (*Result, error) {
	o := options{queueTimeout: DefaultTimeout}
	for _, opt := range opts {
		opt(&o)
	}
	if err := requireLoopback(addr); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("run request: %w", err)
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	// Cancelling ctx closes the connection, which wakes a blocked read.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	res, err := run(conn, req, onBars, o)
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return res, err
}

func run(conn net.Conn, req RunRequest, onBars func([]marketdata.Bar, []Event) ([]Order, error), o options) (*Result, error) {
	// Until the run is accepted the client may be queued behind another run: wait
	// for it, but not for ever.
	if err := conn.SetDeadline(time.Now().Add(o.queueTimeout)); err != nil {
		return nil, err
	}
	if err := writeMessage(conn, runMessage{Run: req}); err != nil {
		return nil, fmt.Errorf("sending the run request: %w", err)
	}
	var start startMessage
	if err := readMessage(conn, &start); err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return nil, fmt.Errorf("the trader did not accept the run within %s (it may be busy with another run): %w", o.queueTimeout, err)
		}
		return nil, fmt.Errorf("waiting for Trader to accept the run: %w", err)
	}
	switch {
	case start.Error != "":
		return nil, fmt.Errorf("the trader refused the run: %s", start.Error)
	case start.Accepted == nil || start.Accepted.RunID == "":
		return nil, errors.New("the trader's answer is neither accepted nor an error")
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return follow(conn, onBars, o)
}

// follow answers Trader's messages until its result.
func follow(conn net.Conn, onBars func([]marketdata.Bar, []Event) ([]Order, error), o options) (*Result, error) {
	for {
		var in barsMessage
		if err := readMessage(conn, &in); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("the trader closed the connection before the result")
			}
			return nil, fmt.Errorf("reading from the trader: %w", err)
		}
		if in.Result != nil {
			if in.Bars != nil || in.Events != nil || in.Warmup {
				return nil, errors.New("a result message carries nothing else")
			}
			return in.Result, nil
		}
		if in.Events == nil {
			return nil, errors.New("request has no events list")
		}
		if in.Warmup {
			if o.warm != nil {
				o.warm(in.Bars)
			}
			if err := writeMessage(conn, ordersMessage{Orders: []Order{}}); err != nil {
				return nil, err
			}
			continue
		}
		orders, err := onBars(in.Bars, in.Events)
		if err != nil {
			return nil, err
		}
		if orders == nil {
			orders = []Order{} // an empty list on the wire, not null
		}
		if err := writeMessage(conn, ordersMessage{Orders: orders}); err != nil {
			return nil, err
		}
	}
}
