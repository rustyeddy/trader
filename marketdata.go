package trader

import (
	"context"
	"iter"
	"time"

	"github.com/rustyeddy/trader/num"
)

type ohlc struct {
	o, h, l, c num.Price
	v          int
}

// bar is one observation: an instrument's OHLCV for one interval. Time is when
// the interval opened. availableAt is when the bar is complete, the earliest a
// consumer may see it; whoever builds the bar sets it, and consumers only read it.
type bar struct {
	instrument string
	time.Time
	ohlc
	availableAt time.Time
}

type marketData struct {
	instrument string
	interval   string

	data []bar
}

// bars returns an iterator over the bars in order. If ctx is cancelled the
// iterator yields ctx.Err() once and stops, even after the last bar, so a
// consumer that reads ahead (frames) still learns of it before publishing what
// it holds. No goroutine is involved, so breaking out of the loop early leaks
// nothing.
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
		if err := ctx.Err(); err != nil {
			yield(bar{}, err)
		}
	}
}
