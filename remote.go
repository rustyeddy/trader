package trader

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// maxMessage caps one message body, so a garbled length prefix cannot ask for
// gigabytes.
const maxMessage = 16 << 20

// The wire protocol is two JSON messages, each preceded by its length as four
// big-endian bytes. For every frame the trader sends barsMessage and the worker
// answers ordersMessage. Orders are not carried yet: the answer must be empty.
//
// The bars are marketdata.Bar as encoding/json writes it; a worker written in Go
// reads them back into the same type.
type barsMessage struct {
	Bars []marketdata.Bar `json:"bars"`
}

type ordersMessage struct {
	Orders []json.RawMessage `json:"orders"`
}

// writeMessage sends v as one length-prefixed JSON message.
func writeMessage(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(body) > maxMessage {
		return fmt.Errorf("message of %d bytes exceeds %d", len(body), maxMessage)
	}
	buf := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(buf, uint32(len(body)))
	copy(buf[4:], body)
	_, err = w.Write(buf)
	return err
}

// readMessage reads one length-prefixed JSON message into v. It returns io.EOF
// when the peer closed before a message began, and io.ErrUnexpectedEOF when it
// closed in the middle of one.
func readMessage(r io.Reader, v any) error {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n > maxMessage {
		return fmt.Errorf("message of %d bytes exceeds %d", n, maxMessage)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return err
	}
	return json.Unmarshal(body, v)
}

// remoteStrategy is a strategy that runs in another process on this machine. Each
// onBars sends the frame to the worker and waits for its answer, so the engine
// runs exactly as it does for an in-process strategy.
//
// It cannot return an error from onBars, so the first failure is kept (err), the
// strategy goes quiet, and the trader stops the run at once, before using the
// answer.
type remoteStrategy struct {
	id     string
	conn   net.Conn
	failed error
}

var _ strategy = (*remoteStrategy)(nil)
var _ failer = (*remoteStrategy)(nil)

// dialRemote connects to a worker listening at addr, which must be a loopback
// address: nothing here is authenticated or encrypted.
func dialRemote(id, addr string) (*remoteStrategy, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("remote strategy address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("remote strategy address %q is not loopback", addr)
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("remote strategy: %w", err)
	}
	return &remoteStrategy{id: id, conn: conn}, nil
}

func (r *remoteStrategy) name() string { return r.id }

func (r *remoteStrategy) init() error { return nil }

func (r *remoteStrategy) err() error { return r.failed }

// close ends the session; the worker sees the connection close and exits.
func (r *remoteStrategy) close() error { return r.conn.Close() }

// onBars sends the frame and reads the worker's answer, which must hold no
// orders. After a failure it does nothing.
func (r *remoteStrategy) onBars(_ context.Context, bars []marketdata.Bar) []*order {
	if r.failed != nil {
		return nil
	}
	if err := writeMessage(r.conn, barsMessage{Bars: bars}); err != nil {
		r.failed = fmt.Errorf("remote strategy %s: sending bars: %w", r.id, err)
		return nil
	}
	var reply ordersMessage
	if err := readMessage(r.conn, &reply); err != nil {
		r.failed = fmt.Errorf("remote strategy %s: reading orders: %w", r.id, err)
		return nil
	}
	if len(reply.Orders) > 0 {
		r.failed = fmt.Errorf("remote strategy %s: returned %d orders; orders over the wire are not supported yet", r.id, len(reply.Orders))
	}
	return nil
}
