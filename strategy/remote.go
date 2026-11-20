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

// DefaultTimeout is how long Remote waits for the worker to answer one group.
const DefaultTimeout = 30 * time.Second

// Remote is the trader's end of a connection to a worker.
type Remote struct {
	// Timeout is how long OnBars waits for an answer; zero means DefaultTimeout.
	Timeout time.Duration

	conn net.Conn
}

// Dial connects to a worker listening at addr, which must be a loopback address:
// nothing here is authenticated or encrypted.
func Dial(addr string) (*Remote, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("worker address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("worker address %q is not loopback", addr)
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &Remote{conn: conn}, nil
}

// Close ends the session; the worker sees the connection close and stops.
func (r *Remote) Close() error { return r.conn.Close() }

// OnBars sends one group of bars and waits for the answer. It returns an error if
// the worker does not answer within the timeout or ctx ends first, if the
// connection fails, or if the answer holds any orders. After an error the
// connection should be closed.
func (r *Remote) OnBars(ctx context.Context, bars []marketdata.Bar) error {
	timeout := r.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := r.conn.SetDeadline(deadline); err != nil {
		return err
	}
	// Cancelling ctx expires the deadline at once, which wakes a blocked read.
	stop := context.AfterFunc(ctx, func() { _ = r.conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	err := r.exchange(bars)
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, os.ErrDeadlineExceeded):
		// The socket's timer can fire a moment before ctx notices its own deadline.
		if d, ok := ctx.Deadline(); ok && !time.Now().Before(d) {
			return context.DeadlineExceeded
		}
		return fmt.Errorf("worker did not answer within %s: %w", timeout, err)
	}
	return err
}

func (r *Remote) exchange(bars []marketdata.Bar) error {
	if err := writeMessage(r.conn, barsMessage{Bars: bars}); err != nil {
		return fmt.Errorf("sending bars: %w", err)
	}
	var reply ordersMessage
	if err := readMessage(r.conn, &reply); err != nil {
		return fmt.Errorf("reading orders: %w", err)
	}
	if len(reply.Orders) > 0 {
		return fmt.Errorf("worker returned %d orders; orders over the wire are not supported yet", len(reply.Orders))
	}
	return nil
}
