package account

import (
	"fmt"
	"time"

	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/num"
)

// PositionMark is the current valuation price of one open position's
// listing, and when it was observed (ADR-066).
//
// A mark is "as of the reporter's last observation of that listing" —
// for the simulator, the last fill or bar close it saw — not a live
// price. In a multi-instrument account, different listings' marks can
// have different AsOf times; each is the latest observation at or
// before the snapshot's own AsOf.
type PositionMark struct {
	// Listing identifies the open position this mark values.
	Listing ListingKey
	// Price is the valuation price. It must be positive.
	Price num.Price
	// AsOf is when Price was observed. It must be set and must not be
	// after the snapshot's AsOf.
	AsOf time.Time
}

// checkMarks validates marks against the snapshot's positions and
// returns them in positions order. Every mark must name an open
// position, at most once. A position may have no mark: marks are
// optional for a reporter that has none, and a consumer that needs one
// treats its absence as an error, never as zero or AvgPrice.
func checkMarks(positions []runtimeorder.Position, marks []PositionMark, asOf time.Time) ([]PositionMark, error) {
	if len(marks) == 0 {
		return nil, nil
	}
	open := make(map[ListingKey]struct{}, len(positions))
	for _, p := range positions {
		open[KeyOf(p.Listing)] = struct{}{}
	}
	byKey := make(map[ListingKey]PositionMark, len(marks))
	for i, m := range marks {
		if _, ok := open[m.Listing]; !ok {
			return nil, fmt.Errorf("entry %d: no open position in listing %s/%s/%s",
				i, m.Listing.InstrumentID, m.Listing.Provider, m.Listing.Venue)
		}
		if _, dup := byKey[m.Listing]; dup {
			return nil, fmt.Errorf("entry %d: duplicate mark for listing %s/%s/%s",
				i, m.Listing.InstrumentID, m.Listing.Provider, m.Listing.Venue)
		}
		if m.Price.IsZero() {
			return nil, fmt.Errorf("entry %d: price must be positive", i)
		}
		if m.AsOf.IsZero() {
			return nil, fmt.Errorf("entry %d: as-of time must be set", i)
		}
		if m.AsOf.After(asOf) {
			return nil, fmt.Errorf("entry %d: as-of %s is after snapshot as-of %s", i, m.AsOf, asOf)
		}
		byKey[m.Listing] = m
	}
	ordered := make([]PositionMark, 0, len(byKey))
	for _, p := range positions {
		if m, ok := byKey[KeyOf(p.Listing)]; ok {
			ordered = append(ordered, m)
		}
	}
	return ordered, nil
}
