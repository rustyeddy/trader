package design

import "time"

type price int64

type ohlc struct {
	o, h, l, c price
	v          int
}

type timeRange struct {
	start time.Time
	end   time.Time
}

type marketData struct {
	instrument string
	timerange  timeRange
	interval   string

	bars  []ohlc
	index int
}

func (md *marketData) nextBar() (ohlc, bool) {
	if len(md.bars)-1 == md.index {
		return ohlc{}, false
	}
	md.index++
	return md.bars[md.index], true
}
