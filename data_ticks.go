package trader

type RawTick struct {
	timemilli
	Ask    Price
	Bid    Price
	AskVol float32
	BidVol float32
}

func (t RawTick) Mid() Price {
	return BA{Bid: t.Bid, Ask: t.Ask}.Mid()
}

func (t RawTick) Spread() Price {
	return BA{Bid: t.Bid, Ask: t.Ask}.Spread()
}

func (t RawTick) Minute() timemilli {
	return t.timemilli.FloorToMinute()
}

// TimeMS returns the tick timestamp in milliseconds since the Unix epoch.
// Exported for use by sibling packages that need raw tick time.
func (t RawTick) TimeMS() int64 {
	return int64(t.timemilli)
}
