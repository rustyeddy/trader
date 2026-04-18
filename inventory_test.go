package trader

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func TestDataKindString(t *testing.T) {
	t.Parallel()

	require.Equal(t, "ticks", KindTick.String())
	require.Equal(t, "candles", KindCandle.String())
	require.Equal(t, "unknown", KindUnknown.String())
}

func TestNormalizeSource(t *testing.T) {
	t.Parallel()

	require.Equal(t, "dukascopy", normalizeSource("  DUKASCOPY  "))
	require.Equal(t, "candles", normalizeSource("Candles"))
	require.Equal(t, "", normalizeSource("   "))
}

func TestInventoryPutGetHasDelete(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k := Key{Instrument: "EURUSD", Source: "candles", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	a := Asset{Key: k, Exists: true, Complete: true}

	require.False(t, inv.Has(k))

	inv.Put(a)
	require.True(t, inv.Has(k))

	got, ok := inv.Get(k)
	require.True(t, ok)
	require.Equal(t, a, got)

	inv.Delete(k)
	require.False(t, inv.Has(k))

	_, ok = inv.Get(k)
	require.False(t, ok)
}

func TestInventoryKeysListLen(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k1 := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Kind: KindCandle, Year: 2026, Month: 2}
	inv.Put(Asset{Key: k1})
	inv.Put(Asset{Key: k2})

	require.Equal(t, 2, inv.Len())
	require.Len(t, inv.Keys(), 2)
	require.Len(t, inv.List(), 2)
}

func TestInventoryUpdate(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}

	err := inv.Update(k, func(a *Asset) error {
		a.Complete = true
		return nil
	})
	require.ErrorIs(t, err, ErrKeyNotFound)

	inv.Put(Asset{Key: k, Exists: true})
	err = inv.Update(k, func(a *Asset) error {
		a.Complete = true
		return nil
	})
	require.NoError(t, err)

	got, ok := inv.Get(k)
	require.True(t, ok)
	require.True(t, got.Complete)
}

func TestInventoryHasComplete(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}

	// Not present
	require.False(t, inv.HasComplete(k))

	// Present but not complete
	inv.Put(Asset{Key: k, Exists: true, Complete: false})
	require.False(t, inv.HasComplete(k))

	// Present and complete but not exists
	inv.Put(Asset{Key: k, Exists: false, Complete: true})
	require.False(t, inv.HasComplete(k))

	// Present, complete and exists
	inv.Put(Asset{Key: k, Exists: true, Complete: true})
	require.True(t, inv.HasComplete(k))
}

func TestInventoryMissingComplete(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k1 := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k3 := Key{Instrument: "USDJPY", Kind: KindCandle, Year: 2026, Month: 1}

	inv.Put(Asset{Key: k1, Exists: true, Complete: true})
	inv.Put(Asset{Key: k2, Exists: false, Complete: false})
	// k3 not in inventory at all

	missing := inv.MissingComplete([]Key{k1, k2, k3})
	require.Len(t, missing, 2)
	require.Contains(t, missing, k2)
	require.Contains(t, missing, k3)
}
