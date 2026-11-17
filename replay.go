package trader

import (
	"cmp"
	"context"
	"fmt"
	"iter"
	"slices"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// seriesBars returns an iterator over a series' bars in order. If ctx is
// cancelled the iterator yields ctx.Err() once and stops, even after the last
// bar, so a consumer that reads ahead (frames) still learns of it before
// publishing what it holds. No goroutine is involved, so breaking out of the loop
// early leaks nothing.
func seriesBars(ctx context.Context, s marketdata.Series) iter.Seq2[marketdata.Bar, error] {
	return func(yield func(marketdata.Bar, error) bool) {
		for _, b := range s.Bars {
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

// mergedBars delivers the bars of several series as one sequence, each bar once
// it is available: by availableAt, then by instrument name, so the order does not
// depend on the order of series. A bar that opened first can arrive later when
// its interval is longer. The series are marketdata.Series values a reader has
// already loaded; the engine opens no file and knows no provider. backtest.runSeries
// validates them (marketdata.Series.Validate) before calling this; mergedBars does
// not repeat that and checks only what the merge itself relies on, listed below.
//
// A missing bar is simply not delivered: nothing is synthesized or carried
// forward, and an empty series contributes nothing (all empty: no bars). A run has
// one interval, so series of different intervals are refused. Series must be of
// different instruments, each bar must be of its series' instrument, and
// availability must increase strictly within a series; anything else is reported
// as the sequence's only item. A bar's availableAt is used as stored, never
// recomputed from its opening time or the interval.
func mergedBars(ctx context.Context, series ...marketdata.Series) iter.Seq2[marketdata.Bar, error] {
	merged, err := mergeSeries(series)
	if err != nil {
		return func(yield func(marketdata.Bar, error) bool) { yield(marketdata.Bar{}, err) }
	}
	return seriesBars(ctx, marketdata.Series{Bars: merged})
}

func mergeSeries(series []marketdata.Series) ([]marketdata.Bar, error) {
	seen := make(map[string]bool, len(series))
	var merged []marketdata.Bar
	for _, md := range series {
		if seen[md.Instrument] {
			return nil, fmt.Errorf("merge bars: instrument %q given twice", md.Instrument)
		}
		seen[md.Instrument] = true
		if md.Interval != series[0].Interval {
			return nil, fmt.Errorf("merge bars: %s is %q but %s is %q; a run has one interval",
				md.Instrument, md.Interval, series[0].Instrument, series[0].Interval)
		}

		for i, b := range md.Bars {
			if b.Instrument != md.Instrument {
				return nil, fmt.Errorf("merge bars: a %q bar at %s is in the %q series", b.Instrument, b.Time.Format(barTimeLayout), md.Instrument)
			}
			if b.AvailableAt.IsZero() {
				return nil, fmt.Errorf("merge bars: %s bar at %s has no availability", b.Instrument, b.Time.Format(barTimeLayout))
			}
			if i > 0 && !b.AvailableAt.After(md.Bars[i-1].AvailableAt) {
				return nil, fmt.Errorf("merge bars: %s bar at %s is not available after the one before it", b.Instrument, b.Time.Format(barTimeLayout))
			}
		}
		merged = append(merged, md.Bars...)
	}
	slices.SortFunc(merged, func(a, b marketdata.Bar) int {
		return cmp.Or(a.AvailableAt.Compare(b.AvailableAt), cmp.Compare(a.Instrument, b.Instrument))
	})
	return merged, nil
}

// frame is the bars that became available at one moment, one per instrument, in
// instrument order. It holds only fresh bars: an instrument with no bar at that
// moment is absent, never represented by an older value.
type frame struct {
	availableAt time.Time
	bars        []marketdata.Bar
}

// frames groups the sequence from mergedBars by availableAt. In a replay a frame
// is complete when the next bar is later, so it is delivered then, or when the
// sequence ends. On an error the frame still being built is dropped, since it
// may be incomplete.
func frames(bars iter.Seq2[marketdata.Bar, error]) iter.Seq2[frame, error] {
	return func(yield func(frame, error) bool) {
		var pending frame
		for b, err := range bars {
			if err != nil {
				yield(frame{}, err)
				return
			}
			if len(pending.bars) > 0 && !b.AvailableAt.Equal(pending.availableAt) {
				if !yield(pending, nil) {
					return
				}
				pending = frame{}
			}
			pending.availableAt = b.AvailableAt
			pending.bars = append(pending.bars, b)
		}
		if len(pending.bars) > 0 {
			yield(pending, nil)
		}
	}
}
