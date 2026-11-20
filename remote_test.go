package trader

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"testing"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessage_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeMessage(&buf, barsMessage{Bars: []marketdata.Bar{{Instrument: "EURUSD", Volume: 7}}}))
	assert.Equal(t, uint32(buf.Len()-4), binary.BigEndian.Uint32(buf.Bytes()), "the prefix is the body length")

	var got barsMessage
	require.NoError(t, readMessage(&buf, &got))
	require.Len(t, got.Bars, 1)
	assert.Equal(t, "EURUSD", got.Bars[0].Instrument)
	assert.Equal(t, int64(7), got.Bars[0].Volume)
}

func TestReadMessage_Errors(t *testing.T) {
	var v ordersMessage
	frameOf := func(body string) []byte {
		return append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), body...)
	}
	t.Run("closed before a message is a clean EOF", func(t *testing.T) {
		assert.ErrorIs(t, readMessage(bytes.NewReader(nil), &v), io.EOF)
	})
	t.Run("closed inside the prefix", func(t *testing.T) {
		assert.ErrorIs(t, readMessage(bytes.NewReader([]byte{0, 0}), &v), io.ErrUnexpectedEOF)
	})
	t.Run("closed inside the body", func(t *testing.T) {
		assert.ErrorIs(t, readMessage(bytes.NewReader(frameOf(`{"orders":[]}`)[:8]), &v), io.ErrUnexpectedEOF)
	})
	t.Run("a body that is not JSON", func(t *testing.T) {
		assert.Error(t, readMessage(bytes.NewReader(frameOf(`nope`)), &v))
	})
	t.Run("a length over the cap is refused before it is read", func(t *testing.T) {
		huge := binary.BigEndian.AppendUint32(nil, maxMessage+1)
		assert.ErrorContains(t, readMessage(bytes.NewReader(huge), &v), "exceeds")
	})
}

func TestDialRemote_LoopbackOnly(t *testing.T) {
	for _, addr := range []string{"8.8.8.8:9100", "example.com:9100", "0.0.0.0:9100", "no-port"} {
		_, err := dialRemote("x", addr)
		assert.Error(t, err, addr)
	}
}

func TestDialRemote_NothingListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	_, err = dialRemote("x", addr)
	assert.ErrorContains(t, err, "remote strategy")
}

func TestRemoteStrategy_OnBars(t *testing.T) {
	ctx := context.Background()
	bars := []marketdata.Bar{{Instrument: "EURUSD", Volume: 1}}

	t.Run("an empty answer is no orders and no error", func(t *testing.T) {
		w := startWorker(t, workerOpts{})
		r, err := dialRemote("remote", w.addr)
		require.NoError(t, err)
		defer func() { _ = r.close() }()

		assert.Equal(t, "remote", r.name())
		require.NoError(t, r.init())
		assert.Empty(t, r.onBars(ctx, bars))
		assert.Empty(t, r.onBars(ctx, bars))
		assert.NoError(t, r.err())
		require.NoError(t, r.close())
		assert.Len(t, w.finish(), 2, "the worker saw both groups")
	})
	t.Run("orders in the answer fail the strategy", func(t *testing.T) {
		w := startWorker(t, workerOpts{orders: []json.RawMessage{json.RawMessage(`{}`)}})
		r, err := dialRemote("remote", w.addr)
		require.NoError(t, err)
		defer func() { _ = r.close() }()

		assert.Empty(t, r.onBars(ctx, bars))
		assert.ErrorContains(t, r.err(), "not supported")
	})
	t.Run("a dead worker fails the strategy and then it is quiet", func(t *testing.T) {
		w := startWorker(t, workerOpts{dropAfter: 1})
		r, err := dialRemote("remote", w.addr)
		require.NoError(t, err)
		defer func() { _ = r.close() }()

		assert.Empty(t, r.onBars(ctx, bars))
		require.NoError(t, r.err(), "the first group was answered")
		assert.Empty(t, r.onBars(ctx, bars))
		require.Error(t, r.err())
		first := r.err()
		assert.Empty(t, r.onBars(ctx, bars), "no further traffic after a failure")
		assert.Same(t, first, r.err(), "the first failure is the one kept")
	})
}
