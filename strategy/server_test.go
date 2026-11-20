package strategy

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServe(t *testing.T) {
	t.Run("answers each group with no orders until the peer closes", func(t *testing.T) {
		a, b := net.Pipe()
		var got [][]marketdata.Bar
		done := make(chan error, 1)
		go func() {
			done <- Serve(b, func(bars []marketdata.Bar) error { got = append(got, bars); return nil })
		}()

		for i := 0; i < 3; i++ {
			require.NoError(t, writeMessage(a, barsMessage{Bars: oneBar}))
			var out ordersMessage
			require.NoError(t, readMessage(a, &out))
			assert.NotNil(t, out.Orders, "an empty list on the wire, not null")
			assert.Empty(t, out.Orders)
		}
		require.NoError(t, a.Close())
		require.NoError(t, <-done, "a closed connection is a clean end")
		assert.Len(t, got, 3)
	})
	t.Run("an error from onBars ends it without an answer", func(t *testing.T) {
		a, b := net.Pipe()
		defer func() { _ = a.Close() }()
		boom := errors.New("boom")
		done := make(chan error, 1)
		go func() { done <- Serve(b, func([]marketdata.Bar) error { return boom }) }()

		require.NoError(t, writeMessage(a, barsMessage{Bars: oneBar}))
		assert.ErrorIs(t, <-done, boom)
	})
	t.Run("a garbled message is an error", func(t *testing.T) {
		a, b := net.Pipe()
		done := make(chan error, 1)
		go func() { done <- Serve(b, func([]marketdata.Bar) error { return nil }) }()

		_, err := a.Write([]byte{0, 0, 0, 4, 'n', 'o', 'p', 'e'})
		require.NoError(t, err)
		assert.Error(t, <-done)
	})
}

func TestListenAndServe(t *testing.T) {
	t.Run("serves one connection on the bound address", func(t *testing.T) {
		addrs := make(chan net.Addr, 1)
		groups := 0
		done := make(chan error, 1)
		go func() {
			done <- ListenAndServe("127.0.0.1:0", func(a net.Addr) { addrs <- a },
				func([]marketdata.Bar) error { groups++; return nil })
		}()

		c, err := Dial((<-addrs).String())
		require.NoError(t, err)
		require.NoError(t, c.OnBars(context.Background(), oneBar))
		require.NoError(t, c.OnBars(context.Background(), oneBar))
		require.NoError(t, c.Close())
		require.NoError(t, <-done)
		assert.Equal(t, 2, groups)
	})
	t.Run("a nil ready is allowed", func(t *testing.T) {
		done := make(chan error, 1)
		go func() { done <- ListenAndServe("127.0.0.1:0", nil, func([]marketdata.Bar) error { return nil }) }()
		select {
		case err := <-done:
			t.Fatalf("returned early: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
	})
	t.Run("a non-loopback address is refused", func(t *testing.T) {
		for _, addr := range []string{"0.0.0.0:0", ":0", "8.8.8.8:9100", "no-port"} {
			err := ListenAndServe(addr, nil, func([]marketdata.Bar) error { return nil })
			assert.Error(t, err, addr)
		}
	})
	t.Run("an address in use is an error", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer func() { _ = ln.Close() }()
		assert.Error(t, ListenAndServe(ln.Addr().String(), nil, func([]marketdata.Bar) error { return nil }))
	})
}
