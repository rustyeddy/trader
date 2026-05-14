package trader

import "sync"

type lotState int

const (
	LotNone lotState = iota
	LotOpenRequested
	LotOpen
	LotCloseRequested
	LotClosed
)

type Lot struct {
	*TradeCommon
	EntryPrice     Price
	EntryTime      Timestamp
	OriginalUnits  Units
	RemainingUnits Units
	State          lotState
}

type LotBook struct {
	mu   sync.RWMutex
	lots map[string]*Lot
}

func (lb *LotBook) All() map[string]*Lot {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	if lb.lots == nil {
		return nil
	}
	out := make(map[string]*Lot, len(lb.lots))
	for id, lot := range lb.lots {
		out[id] = lot
	}
	return out
}

func (lb *LotBook) Len() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return len(lb.lots)
}

func (lb *LotBook) Add(lot *Lot) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.lots == nil {
		lb.lots = make(map[string]*Lot)
	}
	lb.lots[lot.ID] = lot
}

func (lb *LotBook) Delete(id string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.lots == nil {
		return
	}
	delete(lb.lots, id)
}

func (lb *LotBook) Range(fn func(*Lot) error) error {
	lb.mu.RLock()
	lots := make([]*Lot, 0, len(lb.lots))
	for _, lot := range lb.lots {
		lots = append(lots, lot)
	}
	lb.mu.RUnlock()
	for _, lot := range lots {
		if err := fn(lot); err != nil {
			return err
		}
	}
	return nil
}

func (lb *LotBook) Slice() []*Lot {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	out := make([]*Lot, 0, len(lb.lots))
	for _, lot := range lb.lots {
		out = append(out, lot)
	}
	return out
}
