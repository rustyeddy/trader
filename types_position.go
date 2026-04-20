package trader

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
