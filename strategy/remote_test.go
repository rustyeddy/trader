package strategy

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawWorker listens on loopback and hands the one connection it accepts to
// handle, which speaks the protocol by hand.
func rawWorker(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		handle(conn)
	}()
	return ln.Addr().String()
}

var oneBar = []marketdata.Bar{{Instrument: "EURUSD", Volume: 1}}

func TestDial_LoopbackOnly(t *testing.T) {
	for _, addr := range []string{"8.8.8.8:9100", "example.com:9100", "0.0.0.0:9100", "no-port"} {
		_, err := Dial(addr)
		assert.Error(t, err, addr)
	}
}

func TestDial_NothingListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	_, err = Dial(addr)
	assert.Error(t, err)
}

func TestRemote_OnBars(t *testing.T) {
	ctx := context.Background()

	t.Run("an empty answer is success", func(t *testing.T) {
		addr := rawWorker(t, func(c net.Conn) {
			for {
				var in barsMessage
				if readMessage(c, &in) != nil {
					return
				}
				_ = writeMessage(c, ordersMessage{})
			}
		})
		r, err := Dial(addr)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()
		assert.NoError(t, r.OnBars(ctx, oneBar))
		assert.NoError(t, r.OnBars(ctx, oneBar))
	})
	t.Run("orders in the answer are an error", func(t *testing.T) {
		addr := rawWorker(t, func(c net.Conn) {
			var in barsMessage
			_ = readMessage(c, &in)
			_ = writeMessage(c, ordersMessage{Orders: []json.RawMessage{json.RawMessage(`{}`)}})
		})
		r, err := Dial(addr)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()
		assert.ErrorContains(t, r.OnBars(ctx, oneBar), "not supported")
	})
	t.Run("a worker that hangs up is an error", func(t *testing.T) {
		addr := rawWorker(t, func(c net.Conn) {
			var in barsMessage
			_ = readMessage(c, &in)
		})
		r, err := Dial(addr)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()
		assert.ErrorContains(t, r.OnBars(ctx, oneBar), "reading orders")
	})
	t.Run("a silent worker times out", func(t *testing.T) {
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		addr := rawWorker(t, func(c net.Conn) {
			var in barsMessage
			_ = readMessage(c, &in)
			<-release // connected, took the bars, never answers
		})
		r, err := Dial(addr)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()
		r.Timeout = 50 * time.Millisecond

		start := time.Now()
		err = r.OnBars(ctx, oneBar)
		assert.ErrorContains(t, err, "did not answer within")
		assert.Less(t, time.Since(start), 2*time.Second)
	})
	t.Run("cancelling the context stops a silent worker's wait", func(t *testing.T) {
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		addr := rawWorker(t, func(c net.Conn) {
			var in barsMessage
			_ = readMessage(c, &in)
			<-release
		})
		r, err := Dial(addr) // default 30 s timeout: only the cancel can end this in time
		require.NoError(t, err)
		defer func() { _ = r.Close() }()

		cctx, cancel := context.WithCancel(ctx)
		time.AfterFunc(50*time.Millisecond, cancel)
		start := time.Now()
		assert.ErrorIs(t, r.OnBars(cctx, oneBar), context.Canceled)
		assert.Less(t, time.Since(start), 2*time.Second)
	})
	t.Run("a context deadline shorter than the timeout wins", func(t *testing.T) {
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		addr := rawWorker(t, func(c net.Conn) {
			var in barsMessage
			_ = readMessage(c, &in)
			<-release
		})
		r, err := Dial(addr)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()

		cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		assert.ErrorIs(t, r.OnBars(cctx, oneBar), context.DeadlineExceeded)
	})
}
