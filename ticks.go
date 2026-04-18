package trader

import (
	"regexp"

	"github.com/rustyeddy/trader/types"
)

type RawTick struct {
	types.Timemilli
	Ask    types.Price
	Bid    types.Price
	AskVol float32
	BidVol float32
}

func (t RawTick) Mid() types.Price {
	return types.Price((int64(t.Bid) + int64(t.Ask)) >> 1)
}

func (t RawTick) Spread() types.Price {
	return t.Ask - t.Bid
}

func (t RawTick) Minute() types.Timemilli {
	return types.Timemilli((t.Timemilli / 60_000) * 60_000)
}

var rePath = regexp.MustCompile(`[/\\](\d{4})[/\\](\d{2})[/\\](\d{2})[/\\](\d{2})h_ticks\.bi5$`)
