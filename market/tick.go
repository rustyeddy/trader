package market

import (
	"context"
	"errors"
	"sync"
)

type TickSource interface {
	GetTick(ctx context.Context, instrument string) (Tick, error)
}

type BA struct {
	Bid Price
	Ask Price
}

type Tick struct {
	Instrument string
	Time       Timestamp
	BA
}

func (t Tick) Mid() Price {
	mid := Price((int64(t.Bid) + int64(t.Ask)) / 2)
	return mid
}

func (t Tick) Spread() Price {
	return t.Ask - t.Bid
}

type TickStore struct {
	mu    sync.RWMutex
	ticks map[string]Tick
}

func NewTickStore() *TickStore {
	return &TickStore{ticks: make(map[string]Tick)}
}

func (ps *TickStore) Set(p Tick) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.ticks[p.Instrument] = p
}

func (ps *TickStore) Get(instr string) (Tick, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.ticks[instr]
	if !ok {
		return Tick{}, errors.New("price not found")
	}
	return p, nil
}
