package strategy

import (
	"errors"
	"io"
	"net"

	"github.com/rustyeddy/trader/marketdata"
)

// ListenAndServe listens on the loopback address addr, accepts one connection
// from the trader and serves it (see Serve) until the trader closes it. ready, if
// not nil, is called once with the address actually bound, which is how a caller
// learns the port of "127.0.0.1:0".
func ListenAndServe(addr string, ready func(net.Addr), onBars func(bars []marketdata.Bar, events []Event) ([]Order, error), opts ...Option) error {
	if err := requireLoopback(addr); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()
	if ready != nil {
		ready(ln.Addr())
	}
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return Serve(conn, onBars, opts...)
}

// Serve is the worker's end of a connection. It reads each group of bars, calls
// onBars with it and answers with the orders it returns (none is fine), until
// the trader closes the connection (Serve then returns nil). If onBars returns an
// error Serve returns it without answering. Serve does not close conn.
//
// Bars the trader marks as warm-up never reach onBars: they go to the WithWarmup
// function, if there is one, and are answered with no orders either way.
func Serve(conn net.Conn, onBars func(bars []marketdata.Bar, events []Event) ([]Order, error), opts ...Option) error {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	for {
		var in barsMessage
		if err := readMessage(conn, &in); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if in.Events == nil {
			return errors.New("request has no events list")
		}
		if in.Warmup {
			if o.warm != nil {
				o.warm(in.Bars)
			}
			if err := writeMessage(conn, ordersMessage{Orders: []Order{}}); err != nil {
				return err
			}
			continue
		}
		orders, err := onBars(in.Bars, in.Events)
		if err != nil {
			return err
		}
		if orders == nil {
			orders = []Order{} // an empty list on the wire, not null
		}
		if err := writeMessage(conn, ordersMessage{Orders: orders}); err != nil {
			return err
		}
	}
}

type options struct {
	warm func(bars []marketdata.Bar)
}

// Option changes how Serve or ListenAndServe behaves.
type Option func(*options)

// WithWarmup gives the worker the bars from before the range the trader trades, to
// prefill its indicators: it learns from them and decides nothing, so there is
// nothing to return. Without it those bars are acknowledged and ignored.
func WithWarmup(warm func(bars []marketdata.Bar)) Option {
	return func(o *options) { o.warm = warm }
}
