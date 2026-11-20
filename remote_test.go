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
		assert.Empty(t, r.onBars(ctx, bars))
		assert.Empty(t, r.onBars(ctx, bars))
		assert.NoError(t, r.err())
		require.NoError(t, r.close())
		assert.Len(t, w.finish(), 2, "the worker saw both groups")
	})
	t.Run("a dead worker fails the strategy and then it is quiet", func(t *testing.T) {
		w := startWorker(t, workerOpts{dropOn: 2})
		r, err := dialRemote("remote", w.addr)
		require.NoError(t, err)
		defer func() { _ = r.close() }()

		assert.Empty(t, r.onBars(ctx, bars))
		require.NoError(t, r.err(), "the first group was answered")
		assert.Empty(t, r.onBars(ctx, bars))
		require.ErrorContains(t, r.err(), "remote strategy remote")
		first := r.err()
		assert.Empty(t, r.onBars(ctx, bars), "no further traffic after a failure")
		assert.Same(t, first, r.err(), "the first failure is the one kept")
	})
}
