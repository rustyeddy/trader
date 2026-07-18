package datamanager

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type iterator[T any] interface {
	Next() bool
	Item() T
	Err() error
	Close() error
}

type funcIterator[T any] struct {
	nextFn  func() (T, bool, error)
	closeFn func() error

	cur    T
	err    error
	done   bool
	closed bool
}

func newFuncIterator[T any](nextFn func() (T, bool, error), closeFn func() error) iterator[T] {
	if closeFn == nil {
		closeFn = func() error { return nil }
	}
	return &funcIterator[T]{
		nextFn:  nextFn,
		closeFn: closeFn,
	}
}

func (it *funcIterator[T]) Next() bool {
	if it.closed || it.done || it.err != nil {
		var zero T
		it.cur = zero
		return false
	}

	item, ok, err := it.nextFn()
	if err != nil {
		it.err = err
		var zero T
		it.cur = zero
		return false
	}
	if !ok {
		it.done = true
		var zero T
		it.cur = zero
		return false
	}

	it.cur = item
	return true
}

func (it *funcIterator[T]) Item() T {
	return it.cur
}

func (it *funcIterator[T]) Err() error {
	return it.err
}

func (it *funcIterator[T]) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true
	return it.closeFn()
}

type rangedCandleIterator struct {
	base     *candleSetIterator
	rng      types.TimeRange
	useRange bool

	err    error
	done   bool
	closed bool
}

func newCandleSetIterator(cs *CandleSet, rng types.TimeRange) market.CandleIterator {
	return &rangedCandleIterator{
		base:     cs.Iterator(),
		rng:      rng,
		useRange: rng.Valid(),
	}
}

func (it *rangedCandleIterator) Next() (market.Candle, bool) {
	if it.closed || it.done || it.err != nil {
		return market.Candle{}, false
	}

	for it.base.Next() {
		ts := it.base.Timestamp()
		if it.useRange && !it.rng.Contains(ts) {
			continue
		}

		return it.base.Candle(), true
	}

	it.done = true
	return market.Candle{}, false
}

func (it *rangedCandleIterator) Err() error {
	return it.err
}

func (it *rangedCandleIterator) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true
	return nil
}

type chainedCandleIterator struct {
	iters  []market.CandleIterator
	idx    int
	err    error
	closed bool
}

func newChainedCandleIterator(iters ...market.CandleIterator) market.CandleIterator {
	return &chainedCandleIterator{
		iters: iters,
	}
}

func (it *chainedCandleIterator) Next() (market.Candle, bool) {
	if it.closed || it.err != nil {
		return market.Candle{}, false
	}

	for it.idx < len(it.iters) {
		curIt := it.iters[it.idx]
		if curIt == nil {
			it.idx++
			continue
		}

		if ct, ok := curIt.Next(); ok {
			return ct, true
		}

		if err := curIt.Err(); err != nil {
			it.err = err
			return market.Candle{}, false
		}

		if err := curIt.Close(); err != nil {
			it.err = err
			return market.Candle{}, false
		}

		it.idx++
	}

	return market.Candle{}, false
}

func (it *chainedCandleIterator) Err() error {
	return it.err
}

func (it *chainedCandleIterator) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true

	var firstErr error
	for _, sub := range it.iters {
		if sub == nil {
			continue
		}
		if err := sub.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func closeCandleIterators(iters []market.CandleIterator) error {
	var firstErr error
	for _, it := range iters {
		if it == nil {
			continue
		}
		if err := it.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
