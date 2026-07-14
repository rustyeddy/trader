package datamanager

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type RawTick struct {
	types.TimeMillis
	Ask    types.Price
	Bid    types.Price
	AskVol float32
	BidVol float32
}

func (t RawTick) Mid() types.Price {
	return market.BA{Bid: t.Bid, Ask: t.Ask}.Mid()
}

func (t RawTick) Spread() types.Price {
	return market.BA{Bid: t.Bid, Ask: t.Ask}.Spread()
}

func (t RawTick) Minute() types.TimeMillis {
	return t.TimeMillis.FloorToMinute()
}

// TimeMS returns the tick timestamp in milliseconds since the Unix epoch.
// Exported for use by sibling packages that need raw tick time.
func (t RawTick) TimeMS() int64 {
	return int64(t.TimeMillis)
}
