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

type candleIterator interface {
	Next() bool
	Candle() Candle
	CandleTime() candleTime
	NextCandle() (Candle, bool)
	Timestamp() Timestamp
	Err() error
	Close() error
}

type candleSetIterator struct {
	base     *candleSetIteratorV1
	rng      TimeRange
	useRange bool

	cur    Candle
	ts     Timestamp
	err    error
	done   bool
	closed bool
}

func newCandleSetIterator(cs *candleSet, rng TimeRange) candleIterator {
	return &candleSetIterator{
		base:     cs.Iterator(),
		rng:      rng,
		useRange: rng.Valid(),
	}
}

func (it *candleSetIterator) NextCandle() (Candle, bool) {
	if it.Next() {
		return it.Candle(), true
	}
	return Candle{}, false
}

func (it *candleSetIterator) Next() bool {
	if it.closed || it.done || it.err != nil {
		it.cur = Candle{}
		it.ts = 0
		return false
	}

	for it.base.Next() {
		ts := it.base.Timestamp()
		if it.useRange && !it.rng.Contains(ts) {
			continue
		}

		it.cur = it.base.Candle()
		it.ts = ts
		return true
	}

	it.done = true
	it.cur = Candle{}
	it.ts = 0
	return false
}

func (it *candleSetIterator) Candle() Candle {
	return it.cur
}

func (it *candleSetIterator) CandleTime() candleTime {
	ct := candleTime{
		Candle:    it.Candle(),
		Timestamp: it.Timestamp(),
	}
	return ct
}

func (it *candleSetIterator) Timestamp() Timestamp {
	return it.ts
}

func (it *candleSetIterator) Err() error {
	return it.err
}

func (it *candleSetIterator) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true
	return nil
}

func (it *candleSetIterator) CandleSet() *candleSet {
	return it.base.CandleSet()
}

type chainedCandleIterator struct {
	iters  []candleIterator
	idx    int
	cur    Candle
	ts     Timestamp
	err    error
	closed bool
}

func newChainedCandleIterator(iters ...candleIterator) candleIterator {
	return &chainedCandleIterator{
		iters: iters,
	}
}

func (it *chainedCandleIterator) NextCandle() (Candle, bool) {
	if it.Next() {
		return it.Candle(), true
	}
	return Candle{}, false
}

func (it *chainedCandleIterator) CandleTime() candleTime {
	ct := candleTime{
		Candle:    it.Candle(),
		Timestamp: it.Timestamp(),
	}
	return ct
}

func (it *chainedCandleIterator) Next() bool {
	if it.closed || it.err != nil {
		it.cur = Candle{}
		it.ts = 0
		return false
	}

	for it.idx < len(it.iters) {
		curIt := it.iters[it.idx]
		if curIt == nil {
			it.idx++
			continue
		}

		if curIt.Next() {
			it.cur = curIt.Candle()
			it.ts = curIt.Timestamp()
			return true
		}

		if err := curIt.Err(); err != nil {
			it.err = err
			it.cur = Candle{}
			it.ts = 0
			return false
		}

		if err := curIt.Close(); err != nil {
			it.err = err
			it.cur = Candle{}
			it.ts = 0
			return false
		}

		it.idx++
	}

	it.cur = Candle{}
	it.ts = 0
	return false
}

func (it *chainedCandleIterator) Candle() Candle {
	return it.cur
}

func (it *chainedCandleIterator) Timestamp() Timestamp {
	return it.ts
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
