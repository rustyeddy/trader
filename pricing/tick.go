package pricing

import (
	"context"
	"errors"
	"sync"
	"time"
)

type TickSource interface {
	GetTick(ctx context.Context, instrument string) (Tick, error)
}

type Tick struct {
	Instrument string
	Time       time.Time
	Bid        float64
	Ask        float64
}

func (t Tick) Mid() float64 {
	if t.Bid == 0 && t.Ask == 0 {
		return 0
	}
	return (t.Bid + t.Ask) / 2
}

func (t Tick) Spread() float64 {
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
