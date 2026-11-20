package trader

import (
	"context"
	"net"
	"testing"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialRemote_Errors(t *testing.T) {
	_, err := dialRemote("x", "8.8.8.8:9100")
	assert.ErrorContains(t, err, "remote strategy x")

	ln, lerr := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, lerr)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	_, err = dialRemote("x", addr)
	assert.ErrorContains(t, err, "remote strategy x")
}

func TestRemoteStrategy_OnBars(t *testing.T) {
	ctx := context.Background()
	bars := []marketdata.Bar{{Instrument: "EURUSD", Volume: 1}}

	t.Run("an empty answer is no orders and no error", func(t *testing.T) {
		w := startWorker(t, workerOpts{})
		r, err := dialRemote("remote", w.addr)
		require.NoError(t, err)

		assert.Equal(t, "remote", r.name())
		require.NoError(t, r.init())
		for range 2 {
			orders, err := r.onBars(ctx, bars)
			require.NoError(t, err)
			assert.Empty(t, orders)
		}
		require.NoError(t, r.close())
		assert.Len(t, w.finish(), 2, "the worker saw both groups")
	})
	t.Run("a dead worker is an error", func(t *testing.T) {
		w := startWorker(t, workerOpts{dropOn: 2})
		r, err := dialRemote("remote", w.addr)
		require.NoError(t, err)
		defer func() { _ = r.close() }()

		_, err = r.onBars(ctx, bars)
		require.NoError(t, err, "the first group was answered")
		orders, err := r.onBars(ctx, bars)
		assert.Error(t, err)
		assert.Empty(t, orders)
	})
}
