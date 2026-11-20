package strategy

import (
	"errors"
	"net"
	"testing"

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
