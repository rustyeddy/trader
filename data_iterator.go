package trader

type iterator[T any] interface {
	Next() bool
	Item() T
	Err() error
	Close() error
}

type rawTickIterator = iterator[RawTick]

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

// CandleIterator traverses a sequence of timestamped candles.
type CandleIterator interface {
	Next() (CandleTime, bool)
	Err() error
	Close() error
}

type rangedCandleIterator struct {
	base     *candleSetIterator
	rng      TimeRange
	useRange bool

	err    error
	done   bool
	closed bool
}

func newCandleSetIterator(cs *candleSet, rng TimeRange) CandleIterator {
	return &rangedCandleIterator{
		base:     cs.Iterator(),
		rng:      rng,
		useRange: rng.Valid(),
	}
}

func (it *rangedCandleIterator) Next() (CandleTime, bool) {
	if it.closed || it.done || it.err != nil {
		return CandleTime{}, false
	}

	for it.base.Next() {
		ts := it.base.Timestamp()
		if it.useRange && !it.rng.Contains(ts) {
			continue
		}

		return CandleTime{Candle: it.base.Candle(), Timestamp: ts}, true
	}

	it.done = true
	return CandleTime{}, false
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
	iters  []CandleIterator
	idx    int
	err    error
	closed bool
}

func newChainedCandleIterator(iters ...CandleIterator) CandleIterator {
	return &chainedCandleIterator{
		iters: iters,
	}
}

func (it *chainedCandleIterator) Next() (CandleTime, bool) {
	if it.closed || it.err != nil {
		return CandleTime{}, false
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
			return CandleTime{}, false
		}

		if err := curIt.Close(); err != nil {
			it.err = err
			return CandleTime{}, false
		}

		it.idx++
	}

	return CandleTime{}, false
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

func closeCandleIterators(iters []CandleIterator) error {
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
