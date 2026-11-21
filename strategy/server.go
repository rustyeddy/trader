package strategy

import (
	"io"
	"net"

	"github.com/rustyeddy/trader/marketdata"
)

// ListenAndServe listens on the loopback address addr, accepts one connection
// from the trader and serves it (see Serve) until the trader closes it. ready, if
// not nil, is called once with the address actually bound, which is how a caller
// learns the port of "127.0.0.1:0".
func ListenAndServe(addr string, ready func(net.Addr), onBars func(bars []marketdata.Bar) ([]Order, error)) error {
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
	return Serve(conn, onBars)
}

// Serve is the worker's end of a connection. It reads each group of bars, calls
// onBars with it and answers with the orders it returns (none is fine), until
// the trader closes the connection (Serve then returns nil). If onBars returns an
// error Serve returns it without answering. Serve does not close conn.
func Serve(conn net.Conn, onBars func(bars []marketdata.Bar) ([]Order, error)) error {
	for {
		var in barsMessage
		if err := readMessage(conn, &in); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		orders, err := onBars(in.Bars)
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
