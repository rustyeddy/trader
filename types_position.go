package trader

import "sync"

type positionState int

const (
	PositionNone = iota
	PositionOpenRequested
	PositionOpen
	PositionCloseRequested
	PositionClosed
)

type Position struct {
	*TradeCommon
	FillPrice Price
	FillTime  Timestamp
	State     positionState
}

type Positions struct {
	mu        sync.RWMutex
	positions map[string]*Position
}

func (p *Positions) Positions() map[string]*Position {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.positions == nil {
		return nil
	}
	out := make(map[string]*Position, len(p.positions))
	for id, pos := range p.positions {
		out[id] = pos
	}
	return out
}

func (p *Positions) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.positions)
}

func (p *Positions) Add(pos *Position) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.positions == nil {
		p.positions = make(map[string]*Position)
	}
	p.positions[pos.ID] = pos
}

func (p *Positions) Delete(ID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.positions == nil {
		return
	}
	delete(p.positions, ID)
}

func (p *Positions) Range(fn func(*Position) error) error {
	p.mu.RLock()
	positions := make([]*Position, 0, len(p.positions))
	for _, pos := range p.positions {
		positions = append(positions, pos)
	}
	p.mu.RUnlock()
	for _, pos := range positions {
		if err := fn(pos); err != nil {
			return err
		}
	}
	return nil
}

func (p *Position) TriggerStopLoss(price Price) bool {

	return false
}

func (p *Position) triggerTakeProfit(price Price) bool {

	return false
}

func (p *Position) UnrealizedPL(currentPrice Price, quoteToAccount Price) Money {
	// price delta in quote currency per unit, scaled by PriceScale
	delta := int64(currentPrice - p.FillPrice)

	// If positions always store positive Units, apply direction here.
	// Remove this block if short positions already use negative Units.
	if p.Side == Short {
		delta = -delta
	}

	// units * scaled price delta = quote-currency P/L, still scaled by PriceScale
	plQuote := int64(p.Units) * delta

	// Convert quote currency -> account currency.
	// quoteToAccount is assumed to be a Price-scaled conversion rate.
	// Result is still scaled by PriceScale.
	plAccountPriceScaled := (plQuote * int64(quoteToAccount)) / int64(PriceScale)

	// Convert Price-scaled account amount -> Money-scaled account amount.
	plAccountMoneyScaled := (plAccountPriceScaled * int64(MoneyScale)) / int64(PriceScale)

	return Money(plAccountMoneyScaled)
}
