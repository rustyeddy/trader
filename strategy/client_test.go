package strategy

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
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

// writeRaw sends body as one message, bypassing the types, so a test can send what
// a worker must not.
func writeRaw(c net.Conn, body string) {
	_, _ = c.Write(append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), body...))
}

// answerWith is a worker that answers every group with the given raw body.
func answerWith(t *testing.T, body string) *Client {
	t.Helper()
	addr := rawWorker(t, func(c net.Conn) {
		for {
			var in barsMessage
			if readMessage(c, &in) != nil {
				return
			}
			writeRaw(c, body)
		}
	})
	r, err := Dial(addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestClient_OnBars(t *testing.T) {
	ctx := context.Background()

	t.Run("an empty answer is no orders", func(t *testing.T) {
		r := answerWith(t, `{"orders":[]}`)
		for range 2 {
			orders, err := r.OnBars(ctx, oneBar, nil)
			require.NoError(t, err)
			assert.Empty(t, orders)
		}
	})
	t.Run("requested orders come back in order, prices exact", func(t *testing.T) {
		r := answerWith(t, `{"orders":[
			{"instrument":"EURUSD","action":"open","direction":"long","type":"limit","price":"1.10000","stop_loss":"1.09000","take_profit":"1.12000","ref":"entry"},
			{"instrument":"GBPUSD","action":"close"}]}`)
		orders, err := r.OnBars(ctx, oneBar, nil)
		require.NoError(t, err)
		require.Len(t, orders, 2)
		assert.Equal(t, Order{
			Instrument: "EURUSD", Action: "open", Direction: "long", Type: "limit",
			Price: num.MustParsePrice("1.1"), StopLoss: num.MustParsePrice("1.09"), TakeProfit: num.MustParsePrice("1.12"), Ref: "entry",
		}, orders[0])
		assert.Equal(t, Order{Instrument: "GBPUSD", Action: "close"}, orders[1])
	})
	t.Run("a field the worker may not send is an error", func(t *testing.T) {
		for _, field := range []string{`"units":"1000"`, `"id":"x"`, `"state":"filled"`, `"account":"a"`, `"fill_price":"1.1"`, `"nonsense":1`} {
			r := answerWith(t, `{"orders":[{"instrument":"EURUSD","action":"close",`+field+`}]}`)
			_, err := r.OnBars(ctx, oneBar, nil)
			assert.ErrorContains(t, err, "unknown field", field)
		}
	})
	t.Run("sends the events, and an empty list rather than null when there are none", func(t *testing.T) {
		requests := make(chan barsMessage, 2)
		addr := rawWorker(t, func(c net.Conn) {
			for {
				var in barsMessage
				if readMessage(c, &in) != nil {
					return
				}
				requests <- in
				writeRaw(c, `{"orders":[]}`)
			}
		})
		r, err := Dial(addr)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()

		_, err = r.OnBars(ctx, oneBar, nil)
		require.NoError(t, err)
		first := <-requests
		assert.NotNil(t, first.Events)
		assert.Empty(t, first.Events)

		want := []Event{{Kind: Rejected, Instrument: "EURUSD", Action: "open", Ref: "entry", Cause: "strategy", Reason: "no margin"}}
		_, err = r.OnBars(ctx, oneBar, want)
		require.NoError(t, err)
		assert.Equal(t, want, (<-requests).Events)
	})
	t.Run("an answer without an orders list is an error", func(t *testing.T) {
		for _, body := range []string{`{}`, `{"orders":null}`} {
			r := answerWith(t, body)
			orders, err := r.OnBars(ctx, oneBar, nil)
			assert.ErrorContains(t, err, "no orders list", body)
			assert.Empty(t, orders)
		}
	})
	t.Run("a malformed answer is an error", func(t *testing.T) {
		for _, body := range []string{`nope`, `{"orders":[`, `{"orders":{}}`, `{"orders":[{"price":1.1}]}`, `{"orders":[]} x`, `{"orders":[{"price":"abc"}]}`} {
			r := answerWith(t, body)
			orders, err := r.OnBars(ctx, oneBar, nil)
			assert.Error(t, err, body)
			assert.Empty(t, orders)
		}
	})
	t.Run("a worker that hangs up is an error", func(t *testing.T) {
		addr := rawWorker(t, func(c net.Conn) {
			var in barsMessage
			_ = readMessage(c, &in)
		})
		r, err := Dial(addr)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()
		_, err = r.OnBars(ctx, oneBar, nil)
		assert.ErrorContains(t, err, "reading orders")
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
		_, err = r.OnBars(ctx, oneBar, nil)
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
		_, err = r.OnBars(cctx, oneBar, nil)
		assert.ErrorIs(t, err, context.Canceled)
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
		_, err = r.OnBars(cctx, oneBar, nil)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}
