package strategy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// DefaultTimeout is how long Client waits for the worker to answer one group.
const DefaultTimeout = 30 * time.Second

// Client is the trader's end of a connection to a worker.
type Client struct {
	// Timeout is how long OnBars waits for an answer; zero means DefaultTimeout.
	Timeout time.Duration

	conn net.Conn
}

// Dial connects to a worker listening at addr, which must be a loopback address:
// nothing here is authenticated or encrypted.
func Dial(addr string) (*Client, error) {
	if err := requireLoopback(addr); err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

// Close ends the session; the worker sees the connection close and stops.
func (r *Client) Close() error { return r.conn.Close() }

// OnBars sends one group of bars with the events since the last group (none is
// fine) and waits for the answer: the orders the worker requests, in order,
// possibly none. It returns an error if the worker does not
// answer within the timeout or ctx ends first, if the connection fails, or if the
// answer is not a valid message. After an error the connection should be closed.
func (r *Client) OnBars(ctx context.Context, bars []marketdata.Bar, events []Event) ([]Order, error) {
	if events == nil {
		events = []Event{} // an empty list on the wire, not null
	}
	return r.call(ctx, barsMessage{Bars: bars, Events: events})
}

// Warm sends one group of bars from before the range being traded, for the worker
// to learn from and nothing more: it is a warm-up, not a decision. The worker must
// answer with no orders; an order in the answer is an error, since nothing may be
// traded on warm-up bars. Errors are as for OnBars.
func (r *Client) Warm(ctx context.Context, bars []marketdata.Bar) error {
	orders, err := r.call(ctx, barsMessage{Bars: bars, Events: []Event{}, Warmup: true})
	if err != nil {
		return err
	}
	if len(orders) > 0 {
		return fmt.Errorf("worker answered a warm-up with %d order(s); nothing is traded on warm-up bars", len(orders))
	}
	return nil
}

// call sends msg and waits for the orders it is answered with, within the timeout
// and ctx.
func (r *Client) call(ctx context.Context, msg barsMessage) ([]Order, error) {
	timeout := r.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := r.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	// Cancelling ctx expires the deadline at once, which wakes a blocked read.
	stop := context.AfterFunc(ctx, func() { _ = r.conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	orders, err := r.exchange(msg)
	switch {
	case err == nil:
		return orders, nil
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case errors.Is(err, os.ErrDeadlineExceeded):
		// The socket's timer can fire a moment before ctx notices its own deadline.
		if d, ok := ctx.Deadline(); ok && !time.Now().Before(d) {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("worker did not answer within %s: %w", timeout, err)
	}
	return nil, err
}

func (r *Client) exchange(msg barsMessage) ([]Order, error) {
	if err := writeMessage(r.conn, msg); err != nil {
		return nil, fmt.Errorf("sending bars: %w", err)
	}
	var reply ordersMessage
	if err := readMessage(r.conn, &reply); err != nil {
		return nil, fmt.Errorf("reading orders: %w", err)
	}
	if reply.Orders == nil {
		return nil, errors.New("answer has no orders list; send an empty one, {\"orders\":[]}")
	}
	return reply.Orders, nil
}
