package datamanager

import (
	"time"

	"github.com/rustyeddy/trader/market"
)

// candleSetIterator represents a trader domain type.
type candleSetIterator struct {
	cs  *candleSet
	idx int
}

// Iterator is an internal helper for trader type processing.
func (cs *candleSet) Iterator() *candleSetIterator {
	return &candleSetIterator{
		cs:  cs,
		idx: -1,
	}
}

// NextCandle is an internal helper for trader type processing.
func (it *candleSetIterator) NextCandle() (market.Candle, bool) {
	if it.Next() {
		return it.Candle(), true
	}
	return market.Candle{}, false
}

// Next is an internal helper for trader type processing.
func (it *candleSetIterator) Next() bool {
	n := len(it.cs.Candles)

	for {
		it.idx++
		if it.idx >= n {
			return false
		}
		if market.BitIsSet(it.cs.Valid, it.idx) {
			return true
		}
	}
}

// Candle is an internal helper for trader type processing.
func (it *candleSetIterator) Candle() market.Candle {
	return it.cs.Candles[it.idx]
}

// Index is an internal helper for trader type processing.
func (it *candleSetIterator) Index() int {
	return it.idx
}

// Timestamp is an internal helper for trader type processing.
func (it *candleSetIterator) Timestamp() market.Timestamp {
	return it.cs.Timestamp(it.idx)
}

// Time is an internal helper for trader type processing.
func (it *candleSetIterator) Time() time.Time {
	return it.cs.Time(it.idx)
}

// StartTime is an internal helper for trader type processing.
func (it *candleSetIterator) StartTime() market.Timestamp {
	return it.cs.Start
}

// CandleSet is an internal helper for trader type processing.
func (it *candleSetIterator) CandleSet() *candleSet {
	return it.cs
}
