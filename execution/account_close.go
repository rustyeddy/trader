package execution

import (
	"fmt"
	"sort"

	"github.com/rustyeddy/trader/types"
)

type LotMatch struct {
	Lot   *Lot
	Units types.Units
}

type CloseMatcher interface {
	Match(lots []*Lot, units types.Units) ([]LotMatch, error)
}

// FIFOMatcher closes the oldest open lots first.
type FIFOMatcher struct{}

func (FIFOMatcher) Match(lots []*Lot, units types.Units) ([]LotMatch, error) {
	open := make([]*Lot, 0, len(lots))
	for _, lot := range lots {
		if lot != nil && lot.State == LotOpen && lot.RemainingUnits > 0 {
			open = append(open, lot)
		}
	}

	sort.Slice(open, func(i, j int) bool {
		return open[i].EntryTime < open[j].EntryTime
	})

	var total types.Units
	for _, lot := range open {
		total += lot.RemainingUnits
	}
	if units > total {
		return nil, fmt.Errorf("requested %d units but only %d available", units, total)
	}

	var matches []LotMatch
	remaining := units
	for _, lot := range open {
		if remaining <= 0 {
			break
		}
		take := lot.RemainingUnits
		if take > remaining {
			take = remaining
		}
		matches = append(matches, LotMatch{Lot: lot, Units: take})
		remaining -= take
	}

	return matches, nil
}
