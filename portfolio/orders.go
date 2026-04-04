package portfolio

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type CommonPortfolio struct {
	Instrument *market.Instrument
	Side       types.Side // Long or Sort
	Units      types.Units
	Stop       types.Price
	Take       types.Price
	Reason     string
}

type OpenRequest struct {
	Common       CommonPortfolio
	ID           string
	Price        types.Price
	ReqTimestamp types.Timestamp
}

type CloseRequest struct {
	ID         string
	Instrument *market.Instrument
	Units      types.Units
	Price      types.Price
}
