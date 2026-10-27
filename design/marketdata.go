package main

import (
	"time"
)

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

func (md *marketData) getPriceQ() chan ohlc {
	return make(chan ohlc)

}
