package portfolio

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type OpenRequest struct {
	ID         string
	Instrument *market.Instrument
	Units      types.Units
	Side       types.Side // Long or Sort
	Price      types.Price
	Stop       types.Price
	Take       types.Price
	Reason     string
}

type CloseRequest struct {
	ID         string
	Instrument *market.Instrument
	Units      types.Units
	Price      types.Price
}
