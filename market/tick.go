package market

import (
	"errors"
	"fmt"
	"sync"
)

var ErrTickNotFound = errors.New("tick not found")

// BA represents a trader domain type.
type BA struct {
	Bid Price
	Ask Price
}

// Validate is an internal helper for trader type processing.
func (ba BA) Validate() error {
	if ba.Bid <= 0 {
		return fmt.Errorf("bid must be > 0")
	}
	if ba.Ask <= 0 {
		return fmt.Errorf("ask must be > 0")
	}
	if ba.Ask < ba.Bid {
		return fmt.Errorf("ask must be >= bid")
	}
	return nil
}

// Mid returns the midpoint rounded half-up to the nearest scaled price unit.
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

// Validate is an internal helper for trader type processing.
func (t Tick) Validate() error {
	if NormalizeInstrument(t.Instrument) == "" {
		return fmt.Errorf("tick instrument must not be empty")
	}
	return t.BA.Validate()
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
func (ps *tickStore) Set(p Tick) error {
	if ps == nil {
		return fmt.Errorf("tick store is nil")
	}
	p.Instrument = NormalizeInstrument(p.Instrument)
	if err := p.Validate(); err != nil {
		return err
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.ticks[p.Instrument] = p
	return nil
}

// Get is an internal helper for trader type processing.
func (ps *tickStore) Get(instr string) (Tick, error) {
	if ps == nil {
		return Tick{}, fmt.Errorf("tick store is nil")
	}
	instr = NormalizeInstrument(instr)
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.ticks[instr]
	if !ok {
		return Tick{}, ErrTickNotFound
	}
	return p, nil
}
