package datamanager

import (
	"errors"
	"slices"
	"sync"
)

type Keymap[V any] struct {
	mu sync.RWMutex
	m  map[Key]V
}

var ErrKeyNotFound = errors.New("key not found")

func NewKeymap[V any]() Keymap[V] {
	return Keymap[V]{
		m: make(map[Key]V),
	}
}

func (km *Keymap[V]) Put(key Key, v V) {
	km.mu.Lock()
	defer km.mu.Unlock()

	if km.m == nil {
		km.m = make(map[Key]V)
	}
	km.m[key] = v
}

func (km *Keymap[V]) Get(key Key) (V, bool) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	var zero V
	if km.m == nil {
		return zero, false
	}
	v, ok := km.m[key]
	if !ok {
		return zero, false
	}
	return v, true
}

func (km *Keymap[V]) Has(key Key) bool {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.m == nil {
		return false
	}
	_, ok := km.m[key]
	return ok
}

func (km *Keymap[V]) Delete(key Key) {
	km.mu.Lock()
	defer km.mu.Unlock()

	if km.m == nil {
		return
	}
	delete(km.m, key)
}

func (km *Keymap[V]) Keys() []Key {
	km.mu.RLock()
	defer km.mu.RUnlock()

	out := make([]Key, 0, len(km.m))
	for k := range km.m {
		out = append(out, k)
	}
	sortKeys(out)
	return out
}

func (km *Keymap[V]) List() []V {
	km.mu.RLock()
	defer km.mu.RUnlock()

	keys := make([]Key, 0, len(km.m))
	for k := range km.m {
		keys = append(keys, k)
	}
	sortKeys(keys)

	out := make([]V, 0, len(keys))
	for _, k := range keys {
		out = append(out, km.m[k])
	}
	return out
}

func (km *Keymap[V]) Len() int {
	km.mu.RLock()
	defer km.mu.RUnlock()

	return len(km.m)
}

func (km *Keymap[V]) Update(key Key, fn func(*V) error) error {
	km.mu.Lock()
	defer km.mu.Unlock()

	v, ok := km.m[key]
	if !ok {
		return ErrKeyNotFound
	}
	if err := fn(&v); err != nil {
		return err
	}
	km.m[key] = v
	return nil
}

func (km *Keymap[V]) Range(fn func(Key, V) bool) {
	km.mu.RLock()
	items := km.snapshotLocked()
	km.mu.RUnlock()

	for _, item := range items {
		if !fn(item.key, item.val) {
			return
		}
	}
}

func (km *Keymap[V]) snapshotLocked() []struct {
	key Key
	val V
} {
	keys := make([]Key, 0, len(km.m))
	for k := range km.m {
		keys = append(keys, k)
	}
	sortKeys(keys)

	items := make([]struct {
		key Key
		val V
	}, 0, len(keys))
	for _, k := range keys {
		items = append(items, struct {
			key Key
			val V
		}{key: k, val: km.m[k]})
	}
	return items
}

func sortKeys(keys []Key) {
	slices.SortFunc(keys, func(a, b Key) int {
		return a.compare(b)
	})
}
