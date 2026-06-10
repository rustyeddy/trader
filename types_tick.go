package trader

import (
	"errors"
	"sync"
)

// BA represents a trader domain type.
type BA struct {
	Bid Price
	Ask Price
}

func (ba BA) Mid() Price {
	sum := int64(ba.Bid) + int64(ba.Ask)
	return Price((sum + 1) / 2)
}

func (ba BA) Spread() Price {
	return ba.Ask - ba.Bid
}

// Tick represents a trader domain type.
type Tick struct {
	Instrument string
	Timestamp  Timestamp
	BA
}

// Mid is an internal helper for trader type processing.
func (t Tick) Mid() Price {
	return t.BA.Mid()
}

// Spread is an internal helper for trader type processing.
func (t Tick) Spread() Price {
	return t.BA.Spread()
}

// tickStore represents a trader domain type.
type tickStore struct {
	mu    sync.RWMutex
	ticks map[string]Tick
}

// newTickStore is an internal helper for trader type processing.
func newTickStore() *tickStore {
	return &tickStore{ticks: make(map[string]Tick)}
}

// Set is an internal helper for trader type processing.
func (ps *tickStore) Set(p Tick) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.ticks[p.Instrument] = p
}

// Get is an internal helper for trader type processing.
func (ps *tickStore) Get(instr string) (Tick, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.ticks[instr]
	if !ok {
		return Tick{}, errors.New("price not found")
	}
	return p, nil
}
