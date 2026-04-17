package types

type PositionState int

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

func (p *Position) TriggerStopLoss(price Price) bool {

	return false
}

func (p *Position) triggerTakeProfit(price Price) bool {

	return false
}

func (p *Position) UnrealizedPL(currentPrice Price, quoteToAccount Price) Money {
	plQuote := Money(p.Units) * Money(currentPrice-p.FillPrice)
	return Money(plQuote * Money(quoteToAccount))
}
