package account

import (
	"fmt"
	"sort"
	"sync"

	"github.com/rustyeddy/trader/types"
)

// lotState represents a trader domain type.
type lotState int

const (
	LotNone lotState = iota
	LotOpenRequested
	LotOpen
	LotCloseRequested
	LotClosed
)

// String is an internal helper for trader type processing.
func (s lotState) String() string {
	switch s {
	case LotNone:
		return "none"
	case LotOpenRequested:
		return "open-requested"
	case LotOpen:
		return "open"
	case LotCloseRequested:
		return "close-requested"
	case LotClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Lot represents a trader domain type.
type Lot struct {
	*TradeCommon
	EntryPrice     types.Price
	EntryTime      types.Timestamp
	OriginalUnits  types.Units
	RemainingUnits types.Units
	State          lotState
	// ExtremePrice tracks the highest-high (long) or lowest-low (short) seen
	// since entry. Used by trailing/chandelier exit strategies.
	ExtremePrice types.Price
}

// Clone is an internal helper for trader type processing.
func (lot *Lot) Clone() *Lot {
	if lot == nil {
		return nil
	}
	cp := *lot
	if lot.TradeCommon != nil {
		tradeCommon := *lot.TradeCommon
		cp.TradeCommon = &tradeCommon
	}
	return &cp
}

// Validate is an internal helper for trader type processing.
func (lot *Lot) Validate() error {
	if lot == nil {
		return fmt.Errorf("lot is nil")
	}
	if lot.TradeCommon == nil {
		return fmt.Errorf("lot trade common is nil")
	}
	if lot.ID == "" {
		return fmt.Errorf("lot id must not be empty")
	}
	if lot.Instrument == "" {
		return fmt.Errorf("lot instrument must not be empty")
	}
	if lot.OriginalUnits <= 0 {
		return fmt.Errorf("lot original units must be > 0")
	}
	if lot.RemainingUnits < 0 {
		return fmt.Errorf("lot remaining units must be >= 0")
	}
	if lot.RemainingUnits > lot.OriginalUnits {
		return fmt.Errorf("lot remaining units must not exceed original units")
	}
	return nil
}

// LotBook represents a trader domain type.
type LotBook struct {
	mu   sync.RWMutex
	lots map[string]*Lot
}

// All is an internal helper for trader type processing.
func (lb *LotBook) All() map[string]*Lot {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	if lb.lots == nil {
		return nil
	}
	out := make(map[string]*Lot, len(lb.lots))
	for id, lot := range lb.lots {
		out[id] = lot.Clone()
	}
	return out
}

// Len is an internal helper for trader type processing.
func (lb *LotBook) Len() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return len(lb.lots)
}

// Has is an internal helper for trader type processing.
func (lb *LotBook) Has(id string) bool {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	if lb.lots == nil {
		return false
	}
	_, ok := lb.lots[id]
	return ok
}

// Get is an internal helper for trader type processing.
func (lb *LotBook) Get(id string) *Lot {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	if lb.lots == nil {
		return nil
	}
	lot := lb.lots[id]
	return lot.Clone()
}

// Add is an internal helper for trader type processing.
func (lb *LotBook) Add(lot *Lot) error {
	if err := validateLotBookEntry(lot); err != nil {
		return err
	}
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.lots == nil {
		lb.lots = make(map[string]*Lot)
	}
	lb.lots[lot.ID] = lot
	return nil
}

// Delete is an internal helper for trader type processing.
func (lb *LotBook) Delete(id string) bool {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.lots == nil {
		return false
	}
	_, ok := lb.lots[id]
	delete(lb.lots, id)
	return ok
}

// Range is an internal helper for trader type processing.
func (lb *LotBook) Range(fn func(*Lot) error) error {
	lots := lb.snapshot(false)
	for _, lot := range lots {
		if err := fn(lot); err != nil {
			return err
		}
	}
	return nil
}

// Slice is an internal helper for trader type processing.
func (lb *LotBook) Slice() []*Lot {
	return lb.snapshot(true)
}

func (lb *LotBook) snapshot(clone bool) []*Lot {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	out := make([]*Lot, 0, len(lb.lots))
	for _, lot := range lb.lots {
		if clone {
			out = append(out, lot.Clone())
		} else {
			out = append(out, lot)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left == nil || right == nil {
			return right == nil
		}
		if left.EntryTime != right.EntryTime {
			return left.EntryTime < right.EntryTime
		}
		return left.ID < right.ID
	})
	return out
}

func validateLotBookEntry(lot *Lot) error {
	if lot == nil {
		return fmt.Errorf("lot is nil")
	}
	if lot.TradeCommon == nil {
		return fmt.Errorf("lot trade common is nil")
	}
	if lot.ID == "" {
		return fmt.Errorf("lot id must not be empty")
	}
	return nil
}
