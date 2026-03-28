package data

import (
	"errors"
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func TestKeymapPutAndGet(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k := Key{Instrument: "EURUSD", Source: "candles", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}

	_, ok := km.Get(k)
	require.False(t, ok)

	km.Put(k, 42)
	v, ok := km.Get(k)
	require.True(t, ok)
	require.Equal(t, 42, v)
}

func TestKeymapGetNilMap(t *testing.T) {
	t.Parallel()

	km := Keymap[int]{}
	k := Key{Instrument: "EURUSD"}
	_, ok := km.Get(k)
	require.False(t, ok)
}

func TestKeymapHas(t *testing.T) {
	t.Parallel()

	km := NewKeymap[string]()
	k := Key{Instrument: "GBPUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 2}

	require.False(t, km.Has(k))
	km.Put(k, "hello")
	require.True(t, km.Has(k))
}

func TestKeymapHasNilMap(t *testing.T) {
	t.Parallel()

	km := Keymap[string]{}
	require.False(t, km.Has(Key{}))
}

func TestKeymapDelete(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k := Key{Instrument: "EURUSD", Source: "candles", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 3}
	km.Put(k, 99)
	require.True(t, km.Has(k))

	km.Delete(k)
	require.False(t, km.Has(k))
}

func TestKeymapDeleteNilMap(t *testing.T) {
	t.Parallel()

	// Should not panic on nil map
	km := Keymap[int]{}
	km.Delete(Key{})
}

func TestKeymapKeys(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k1 := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Kind: KindCandle, Year: 2026, Month: 1}

	km.Put(k1, 1)
	km.Put(k2, 2)

	keys := km.Keys()
	require.Len(t, keys, 2)
}

func TestKeymapList(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k1 := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Kind: KindCandle, Year: 2026, Month: 2}

	km.Put(k1, 10)
	km.Put(k2, 20)

	list := km.List()
	require.Len(t, list, 2)
}

func TestKeymapLen(t *testing.T) {
	t.Parallel()

	km := NewKeymap[string]()
	require.Equal(t, 0, km.Len())

	k := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	km.Put(k, "x")
	require.Equal(t, 1, km.Len())
}

func TestKeymapUpdate(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}

	// Update non-existent key returns error
	err := km.Update(k, func(v *int) error {
		*v = 100
		return nil
	})
	require.ErrorIs(t, err, ErrKeyNotFound)

	// After putting, update succeeds
	km.Put(k, 1)
	err = km.Update(k, func(v *int) error {
		*v = 99
		return nil
	})
	require.NoError(t, err)

	v, ok := km.Get(k)
	require.True(t, ok)
	require.Equal(t, 99, v)

	// Update returns fn error
	sentinel := errors.New("fn error")
	err = km.Update(k, func(v *int) error {
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
}

func TestKeymapRange(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	k1 := Key{Instrument: "EURUSD", Kind: KindCandle, Year: 2026, Month: 1}
	k2 := Key{Instrument: "GBPUSD", Kind: KindCandle, Year: 2026, Month: 2}
	km.Put(k1, 1)
	km.Put(k2, 2)

	count := 0
	km.Range(func(k Key, v int) bool {
		count++
		return true
	})
	require.Equal(t, 2, count)

	// Early stop
	count = 0
	km.Range(func(k Key, v int) bool {
		count++
		return false
	})
	require.Equal(t, 1, count)
}
