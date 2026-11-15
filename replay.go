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

// mergedBars delivers the bars of several instruments as one sequence, each bar
// once it is available: by availableAt, then by instrument name, so the order
// does not depend on the order of sets. A bar that opened first can arrive later
// when its interval is longer.
//
// A missing bar is simply not delivered: nothing is synthesized or carried
// forward. Sets must have different instruments and strictly increasing
// availability; anything else is reported as the sequence's only item.
func mergedBars(ctx context.Context, sets ...*marketData) iter.Seq2[marketdata.Bar, error] {
	merged, err := mergeSets(sets)
	if err != nil {
		return func(yield func(marketdata.Bar, error) bool) { yield(marketdata.Bar{}, err) }
	}
	return (&marketData{data: merged}).bars(ctx)
}

func mergeSets(sets []*marketData) ([]marketdata.Bar, error) {
	seen := make(map[string]bool, len(sets))
	var merged []marketdata.Bar
	for _, md := range sets {
		if seen[md.instrument] {
			return nil, fmt.Errorf("merge bars: instrument %q given twice", md.instrument)
		}
		seen[md.instrument] = true

		for i, b := range md.data {
			if b.AvailableAt.IsZero() {
				return nil, fmt.Errorf("merge bars: %s bar at %s has no availability", b.Instrument, b.Time.Format(barTimeLayout))
			}
			if i > 0 && !b.AvailableAt.After(md.data[i-1].AvailableAt) {
				return nil, fmt.Errorf("merge bars: %s bar at %s is not available after the one before it", b.Instrument, b.Time.Format(barTimeLayout))
			}
		}
		merged = append(merged, md.data...)
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
