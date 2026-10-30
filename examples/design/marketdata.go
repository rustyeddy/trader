package main

import (
	"context"
	"iter"
	"math/rand"
	"time"
)

type price int64

const priceScale = 100000

func (p price) Float64() float64 {
	return float64(p) / priceScale
}

type ohlc struct {
	o, h, l, c price
	v          int
}

// bar is one observation: an instrument's OHLCV at a point in time.
type bar struct {
	instrument string
	time.Time
	ohlc
}

type timeRange struct {
	start time.Time
	end   time.Time
}

type marketData struct {
	instrument string
	timerange  timeRange
	interval   string

	data  []bar
	index int
}

// bars returns an iterator over the bars in order. If ctx is cancelled the
// iterator yields ctx.Err() once and stops. No goroutine is involved, so
// breaking out of the loop early leaks nothing.
func (md *marketData) bars(ctx context.Context) iter.Seq2[bar, error] {
	return func(yield func(bar, error) bool) {
		for _, b := range md.data {
			if err := ctx.Err(); err != nil {
				yield(bar{}, err)
				return
			}
			if !yield(b, nil) {
				return
			}
		}
	}
}

// synthesize fills md.data with one daily bar per weekday in md.timerange,
// stamped with md.instrument and the day's date, following a seeded random
// walk so runs are deterministic. Prices are
// integer points (1/100000), e.g. 110000 == 1.10000.
func (md *marketData) synthesize(seed int64, start price) {
	rng := rand.New(rand.NewSource(seed))
	md.data = md.data[:0]
	md.index = 0

	px := start
	step := func() price { return price(rng.Intn(601) - 300) } // +/- 30 pips
	for d := md.timerange.start; !d.After(md.timerange.end); d = d.AddDate(0, 0, 1) {
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		o := px
		c := o + step()
		h, l := max(o, c), min(o, c)
		h += price(rng.Intn(151))
		l -= price(rng.Intn(151))
		md.data = append(md.data, bar{
			instrument: md.instrument,
			Time:       d,
			ohlc:       ohlc{o: o, h: h, l: l, c: c, v: 1000 + rng.Intn(9000)},
		})
		px = c
	}
}
