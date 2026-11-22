package strategy

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServe(t *testing.T) {
	t.Run("answers each group with no orders until the peer closes", func(t *testing.T) {
		a, b := net.Pipe()
		var got [][]marketdata.Bar
		done := make(chan error, 1)
		go func() {
			done <- Serve(b, func(bars []marketdata.Bar, _ []Event) ([]Order, error) { got = append(got, bars); return nil, nil })
		}()

		for i := 0; i < 3; i++ {
			require.NoError(t, writeMessage(a, barsMessage{Bars: oneBar, Events: []Event{}}))
			var out ordersMessage
			require.NoError(t, readMessage(a, &out))
			assert.NotNil(t, out.Orders, "an empty list on the wire, not null")
			assert.Empty(t, out.Orders)
		}
		require.NoError(t, a.Close())
		require.NoError(t, <-done, "a closed connection is a clean end")
		assert.Len(t, got, 3)
	})
	t.Run("answers with the orders onBars returns, in order", func(t *testing.T) {
		a, b := net.Pipe()
		done := make(chan error, 1)
		want := []Order{
			{Instrument: "EURUSD", Action: "open", Direction: "long", Price: num.MustParsePrice("1.1"), StopLoss: num.MustParsePrice("1.09")},
			{Instrument: "EURUSD", Action: "close"},
		}
		go func() { done <- Serve(b, func([]marketdata.Bar, []Event) ([]Order, error) { return want, nil }) }()

		require.NoError(t, writeMessage(a, barsMessage{Bars: oneBar, Events: []Event{}}))
		var out ordersMessage
		require.NoError(t, readMessage(a, &out))
		assert.Equal(t, want, out.Orders)
		require.NoError(t, a.Close())
		require.NoError(t, <-done)
	})
	t.Run("hands the events to onBars", func(t *testing.T) {
		a, b := net.Pipe()
		done := make(chan error, 1)
		var got []Event
		go func() {
			done <- Serve(b, func(_ []marketdata.Bar, events []Event) ([]Order, error) { got = events; return nil, nil })
		}()
		want := []Event{{Kind: Filled, ID: "sim-1", Instrument: "EURUSD", Action: "open", Cause: "strategy"}}
		require.NoError(t, writeMessage(a, barsMessage{Bars: oneBar, Events: want}))
		var out ordersMessage
		require.NoError(t, readMessage(a, &out))
		require.NoError(t, a.Close())
		require.NoError(t, <-done)
		assert.Equal(t, want, got)
	})
	t.Run("a request without an events list is an error", func(t *testing.T) {
		for _, body := range []string{`{"bars":[]}`, `{"bars":[],"events":null}`} {
			a, b := net.Pipe()
			done := make(chan error, 1)
			go func() { done <- Serve(b, func([]marketdata.Bar, []Event) ([]Order, error) { return nil, nil }) }()
			writeRaw(a, body)
			assert.ErrorContains(t, <-done, "no events list", body)
			_ = a.Close()
		}
	})
	t.Run("an error from onBars ends it without an answer", func(t *testing.T) {
		a, b := net.Pipe()
		defer func() { _ = a.Close() }()
		boom := errors.New("boom")
		done := make(chan error, 1)
		go func() { done <- Serve(b, func([]marketdata.Bar, []Event) ([]Order, error) { return nil, boom }) }()

		require.NoError(t, writeMessage(a, barsMessage{Bars: oneBar, Events: []Event{}}))
		assert.ErrorIs(t, <-done, boom)
	})
	t.Run("a garbled message is an error", func(t *testing.T) {
		a, b := net.Pipe()
		done := make(chan error, 1)
		go func() { done <- Serve(b, func([]marketdata.Bar, []Event) ([]Order, error) { return nil, nil }) }()

		_, err := a.Write([]byte{0, 0, 0, 4, 'n', 'o', 'p', 'e'})
		require.NoError(t, err)
		assert.Error(t, <-done)
	})
}

func TestServe_Warmup(t *testing.T) {
	t.Run("warm-up bars go to the warm-up function, never to onBars, and are answered with no orders", func(t *testing.T) {
		a, b := net.Pipe()
		var warmed, decided [][]marketdata.Bar
		done := make(chan error, 1)
		go func() {
			done <- Serve(b, func(bars []marketdata.Bar, _ []Event) ([]Order, error) {
				decided = append(decided, bars)
				return []Order{{Instrument: "EURUSD", Action: "close"}}, nil
			}, WithWarmup(func(bars []marketdata.Bar) { warmed = append(warmed, bars) }))
		}()

		require.NoError(t, writeMessage(a, barsMessage{Bars: oneBar, Events: []Event{}, Warmup: true}))
		var out ordersMessage
		require.NoError(t, readMessage(a, &out))
		assert.NotNil(t, out.Orders)
		assert.Empty(t, out.Orders)
		require.NoError(t, writeMessage(a, barsMessage{Bars: oneBar, Events: []Event{}}))
		require.NoError(t, readMessage(a, &out))
		assert.Len(t, out.Orders, 1, "a trading group is still decided")

		require.NoError(t, a.Close())
		require.NoError(t, <-done)
		assert.Len(t, warmed, 1)
		assert.Len(t, decided, 1)
	})
	t.Run("without a warm-up function the bars are acknowledged and ignored", func(t *testing.T) {
		a, b := net.Pipe()
		calls := 0
		done := make(chan error, 1)
		go func() {
			done <- Serve(b, func([]marketdata.Bar, []Event) ([]Order, error) { calls++; return nil, nil })
		}()

		require.NoError(t, writeMessage(a, barsMessage{Bars: oneBar, Events: []Event{}, Warmup: true}))
		var out ordersMessage
		require.NoError(t, readMessage(a, &out))
		assert.Empty(t, out.Orders)
		require.NoError(t, a.Close())
		require.NoError(t, <-done)
		assert.Zero(t, calls)
	})
}

func TestListenAndServe(t *testing.T) {
	t.Run("serves one connection on the bound address", func(t *testing.T) {
		addrs := make(chan net.Addr, 1)
		groups := 0
		done := make(chan error, 1)
		go func() {
			done <- ListenAndServe("127.0.0.1:0", func(a net.Addr) { addrs <- a },
				func([]marketdata.Bar, []Event) ([]Order, error) { groups++; return nil, nil })
		}()

		c, err := Dial((<-addrs).String())
		require.NoError(t, err)
		_, err = c.OnBars(context.Background(), oneBar, nil)
		require.NoError(t, err)
		_, err = c.OnBars(context.Background(), oneBar, nil)
		require.NoError(t, err)
		require.NoError(t, c.Close())
		require.NoError(t, <-done)
		assert.Equal(t, 2, groups)
	})
	t.Run("a nil ready is allowed", func(t *testing.T) {
		// Without ready the port is unknown, so pick a free one and give it back.
		probe, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := probe.Addr().String()
		require.NoError(t, probe.Close())

		done := make(chan error, 1)
		go func() {
			done <- ListenAndServe(addr, nil, func([]marketdata.Bar, []Event) ([]Order, error) { return nil, nil })
		}()

		var c *Client
		require.Eventually(t, func() bool { c, err = Dial(addr); return err == nil }, 2*time.Second, 10*time.Millisecond)
		_, err = c.OnBars(context.Background(), oneBar, nil)
		require.NoError(t, err)
		require.NoError(t, c.Close())
		require.NoError(t, <-done)
	})
	t.Run("a non-loopback address is refused", func(t *testing.T) {
		for _, addr := range []string{"0.0.0.0:0", ":0", "8.8.8.8:9100", "no-port"} {
			err := ListenAndServe(addr, nil, func([]marketdata.Bar, []Event) ([]Order, error) { return nil, nil })
			assert.Error(t, err, addr)
		}
	})
	t.Run("an address in use is an error", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer func() { _ = ln.Close() }()
		assert.Error(t, ListenAndServe(ln.Addr().String(), nil, func([]marketdata.Bar, []Event) ([]Order, error) { return nil, nil }))
	})
}
