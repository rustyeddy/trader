package portfolio

import "github.com/rustyeddy/trader/types"

type OpenRequest struct {
	Instrument string
	Side       types.Side
	Units      types.Units
	Stop       types.Price
	Take       types.Price
	Reason     string
}

type CloseRequest struct {
	Instrument string
}
