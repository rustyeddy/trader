package strategy

import (
	"encoding/json"
	"io"
	"net"

	"github.com/rustyeddy/trader/marketdata"
)

// Serve is the worker's end of a connection. It reads each group of bars, calls
// onBars with it and answers with no orders, until the trader closes the
// connection (Serve then returns nil). If onBars returns an error Serve returns
// it without answering. Serve does not close conn.
func Serve(conn net.Conn, onBars func(bars []marketdata.Bar) error) error {
	for {
		var in barsMessage
		if err := readMessage(conn, &in); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := onBars(in.Bars); err != nil {
			return err
		}
		if err := writeMessage(conn, ordersMessage{Orders: []json.RawMessage{}}); err != nil {
			return err
		}
	}
}
