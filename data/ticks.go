package data

import (
	"regexp"

	"github.com/rustyeddy/trader/types"

)

type Tick struct {
	types.Timemilli
	Ask    types.Price
	Bid    types.Price
	AskVol float32
	BidVol float32
}

func (t Tick) Mid() types.Price {
	return types.Price((int64(t.Bid) + int64(t.Ask)) >> 1)
}

func (t Tick) Spread() types.Price {
	return t.Ask - t.Bid
}

func (t Tick) Minute() types.Timemilli {
	return types.Timemilli((t.Timemilli / 60_000) * 60_000)
}

var rePath = regexp.MustCompile(`[/\\](\d{4})[/\\](\d{2})[/\\](\d{2})[/\\](\d{2})h_ticks\.bi5$`)

