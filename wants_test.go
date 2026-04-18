package trader

import (
	"errors"
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func TestWantlistPutGetHasDelete(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k := Key{Instrument: "EURUSD", Source: "candles", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	w := Want{Key: k, WantReason: WantMissing}

	require.False(t, wl.Has(k))

	wl.Put(w)
	require.True(t, wl.Has(k))

	got, ok := wl.Get(k)
	require.True(t, ok)
	require.Equal(t, w, got)

	wl.Delete(k)
	require.False(t, wl.Has(k))
}

func TestWantlistKeysListLen(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k1 := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Kind: KindCandle, Year: 2026, Month: 2}
	k3 := Key{Instrument: "USDJPY", Kind: KindCandle, Year: 2026, Month: 3}

	wl.Put(Want{Key: k1, WantReason: WantMissing})
	wl.Put(Want{Key: k2, WantReason: WantIncomplete})
	wl.Put(Want{Key: k3, WantReason: WantStale})

	require.Equal(t, 3, wl.Len())
	require.Len(t, wl.Keys(), 3)
	require.Len(t, wl.List(), 3)
}

func TestWantlistUpdate(t *testing.T) {
	t.Parallel()

	wl := NewWantlist()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}

	// Update non-existent key returns error
	err := wl.Update(k, func(w *Want) error {
		w.WantReason = WantStale
		return nil
	})
	require.ErrorIs(t, err, ErrKeyNotFound)

	// After putting, update succeeds
	wl.Put(Want{Key: k, WantReason: WantMissing})
	err = wl.Update(k, func(w *Want) error {
		w.WantReason = WantStale
		return nil
	})
	require.NoError(t, err)

	got, ok := wl.Get(k)
	require.True(t, ok)
	require.Equal(t, WantStale, got.WantReason)

	// Propagates fn error
	sentinel := errors.New("update failed")
	err = wl.Update(k, func(w *Want) error {
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
}
