package trader

import (
	"context"
	"iter"

	"github.com/rustyeddy/trader/marketdata"
)

type ohlc = marketdata.OHLC
type bar = marketdata.Bar

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
