package trader

import (
	"context"
	"iter"

	"github.com/rustyeddy/trader/marketdata"
)

type marketData struct {
	instrument string
	interval   string

	data []marketdata.Bar
}

// bars returns an iterator over the bars in order. If ctx is cancelled the
// iterator yields ctx.Err() once and stops, even after the last bar, so a
// consumer that reads ahead (frames) still learns of it before publishing what
// it holds. No goroutine is involved, so breaking out of the loop early leaks
// nothing.
func (md *marketData) bars(ctx context.Context) iter.Seq2[marketdata.Bar, error] {
	return func(yield func(marketdata.Bar, error) bool) {
		for _, b := range md.data {
			if err := ctx.Err(); err != nil {
				yield(marketdata.Bar{}, err)
				return
			}
			if !yield(b, nil) {
				return
			}
		}
		if err := ctx.Err(); err != nil {
			yield(marketdata.Bar{}, err)
		}
	}
}
