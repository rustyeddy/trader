package portfolio

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type Position struct {
	ID         string
	Instrument *market.Instrument
	Side       types.Side
	Units      types.Units
	Stop       types.Price
	Take       types.Price
	Price      types.Price
	Timestamp  types.Timestamp
}

type Positions struct {
	positions map[string]Position
}

func (p *Positions) Add(pos Position) {
	p.positions[pos.ID] = pos
}

func (p *Positions) Delete(ID string) {
	delete(p.positions, ID)
}
