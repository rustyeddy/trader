package trader

import (
	"errors"
	"sync"
)

// BA defines the BA type.
type BA struct {
	Bid Price
	Ask Price
}

// Tick defines the Tick type.
type Tick struct {
	Instrument string
	Timestamp  Timestamp
	BA
}

// Mid performs Mid.
func (t Tick) Mid() Price {
	sum := int64(t.Bid) + int64(t.Ask)
	mid := (sum + 1) / 2 // round half up
	return Price(mid)
}

// Spread performs Spread.
func (t Tick) Spread() Price {
	return t.Ask - t.Bid
}

// tickStore defines the tickStore type.
type tickStore struct {
	mu    sync.RWMutex
	ticks map[string]Tick
}

// newTickStore performs newTickStore.
func newTickStore() *tickStore {
	return &tickStore{ticks: make(map[string]Tick)}
}

// Set performs Set.
func (ps *tickStore) Set(p Tick) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.ticks[p.Instrument] = p
}

// Get performs Get.
func (ps *tickStore) Get(instr string) (Tick, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.ticks[instr]
	if !ok {
		return Tick{}, errors.New("price not found")
	}
	return p, nil
}
