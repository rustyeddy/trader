package portfolio

import (
	"github.com/rustyeddy/trader/types"
)

type PositionState int

const (
	PositionNone = iota
	PositionOpenRequested
	PositionOpen
	PositionCloseRequested
	PositionClosed
)

type Position struct {
	Common    *TradeCommon
	FillPrice types.Price
	FillTime  types.Timestamp
	State     PositionState
}

type Positions struct {
	positions map[string]*Position
}

func (p *Positions) Positions() map[string]*Position {
	return p.positions
}

func (p *Positions) Len() int {
	return len(p.positions)
}

func (p *Positions) Add(pos *Position) {
	if p.positions == nil {
		p.positions = make(map[string]*Position)
	}
	p.positions[pos.Common.ID] = pos
}

func (p *Positions) Delete(ID string) {
	if p.positions == nil {
		return
	}
	delete(p.positions, ID)
}

func (p *Positions) Range(fn func(*Position) error) error {
	for _, pos := range p.positions {
		if err := fn(pos); err != nil {
			return err
		}
	}
	return nil
}

func (p *Position) TriggerStopLoss(price types.Price) bool {

	return false
}

func (p *Position) triggerTakeProfit(price types.Price) bool {

	return false
}

func (p *Position) UnrealizedPL(currentPrice types.Price, quoteToAccount types.Price) types.Money {
	plQuote := types.Money(p.Common.Units) * types.Money(currentPrice-p.FillPrice)
	return types.Money(plQuote * types.Money(quoteToAccount))
}
