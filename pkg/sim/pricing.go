// pkg/sim/pricing.go
package sim

import (
	"errors"
	"sync"
	"github.com/rustyeddy/trader/pkg/broker"
)

type PriceStore struct {
	mu     sync.RWMutex
	prices map[string]broker.Price
}

func NewPriceStore() *PriceStore {
	return &PriceStore{prices: make(map[string]broker.Price)}
}

func (ps *PriceStore) Set(p broker.Price) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.prices[p.Instrument] = p
}

func (ps *PriceStore) Get(instr string) (broker.Price, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.prices[instr]
	if !ok {
		return broker.Price{}, errors.New("price not found")
	}
	return p, nil
}
