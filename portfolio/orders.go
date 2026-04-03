package portfolio

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type OpenRequest struct {
	*market.Instrument
	types.Units
	types.Side // Long or Sort
	types.Price
	Stop   types.Price
	Take   types.Price
	Reason string
	Count  int
}

type CloseRequest struct {
	*market.Instrument
	types.Units
	types.Price
	Count int
}
