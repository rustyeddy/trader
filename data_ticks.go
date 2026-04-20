package trader

import (
	"regexp"
)

type RawTick struct {
	timemilli
	Ask    Price
	Bid    Price
	AskVol float32
	BidVol float32
}

func (t RawTick) Mid() Price {
	return Price((int64(t.Bid) + int64(t.Ask)) >> 1)
}

func (t RawTick) Spread() Price {
	return t.Ask - t.Bid
}

func (t RawTick) Minute() timemilli {
	return timemilli((t.timemilli / 60_000) * 60_000)
}

var rePath = regexp.MustCompile(`[/\\](\d{4})[/\\](\d{2})[/\\](\d{2})[/\\](\d{2})h_ticks\.bi5$`)
