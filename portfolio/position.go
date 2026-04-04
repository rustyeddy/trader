package portfolio

import (
	"github.com/rustyeddy/trader/types"
)

type Position struct {
	ID        string
	Common    CommonPortfolio
	FillPrice types.Price
	FillTime  types.Timestamp
}

type Positions struct {
	positions map[string]*Position
}

func (p *Positions) Add(pos *Position) {
	if p.positions == nil {
		p.positions = make(map[string]*Position)
	}
	p.positions[pos.ID] = pos
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
