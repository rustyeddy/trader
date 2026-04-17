package data

import (
	"github.com/rustyeddy/trader/types"
)

type Iterator[T any] interface {
	Next() bool
	Item() T
	Err() error
	Close() error
}

type TickIterator = Iterator[Tick]

type funcIterator[T any] struct {
	nextFn  func() (T, bool, error)
	closeFn func() error

	cur    T
	err    error
	done   bool
	closed bool
}

func NewFuncIterator[T any](nextFn func() (T, bool, error), closeFn func() error) Iterator[T] {
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

type CandleIterator interface {
	Next() bool
	Candle() types.Candle
	CandleTime() types.CandleTime
	NextCandle() (types.Candle, bool)
	Timestamp() types.Timestamp
	Err() error
	Close() error
}

type candleSetIterator struct {
	base     *types.Iterator
	rng      types.TimeRange
	useRange bool

	cur    types.Candle
	ts     types.Timestamp
	err    error
	done   bool
	closed bool
}

func NewCandleSetIterator(cs *types.CandleSet, rng types.TimeRange) CandleIterator {
	return &candleSetIterator{
		base:     cs.Iterator(),
		rng:      rng,
		useRange: rng.Valid(),
	}
}

func (it *candleSetIterator) NextCandle() (types.Candle, bool) {
	if it.Next() {
		return it.Candle(), true
	}
	return types.Candle{}, false
}

func (it *candleSetIterator) Next() bool {
	if it.closed || it.done || it.err != nil {
		it.cur = types.Candle{}
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
	it.cur = types.Candle{}
	it.ts = 0
	return false
}

func (it *candleSetIterator) Candle() types.Candle {
	return it.cur
}

func (it *candleSetIterator) CandleTime() types.CandleTime {
	ct := types.CandleTime{
		Candle:    it.Candle(),
		Timestamp: it.Timestamp(),
	}
	return ct
}

func (it *candleSetIterator) Timestamp() types.Timestamp {
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

type chainedCandleIterator struct {
	iters  []CandleIterator
	idx    int
	cur    types.Candle
	ts     types.Timestamp
	err    error
	closed bool
}

func NewChainedCandleIterator(iters ...CandleIterator) CandleIterator {
	return &chainedCandleIterator{
		iters: iters,
	}
}

func (it *chainedCandleIterator) NextCandle() (types.Candle, bool) {
	if it.Next() {
		return it.Candle(), true
	}
	return types.Candle{}, false
}

func (it *chainedCandleIterator) CandleTime() types.CandleTime {
	ct := types.CandleTime{
		Candle:    it.Candle(),
		Timestamp: it.Timestamp(),
	}
	return ct
}

func (it *chainedCandleIterator) Next() bool {
	if it.closed || it.err != nil {
		it.cur = types.Candle{}
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
			it.cur = types.Candle{}
			it.ts = 0
			return false
		}

		if err := curIt.Close(); err != nil {
			it.err = err
			it.cur = types.Candle{}
			it.ts = 0
			return false
		}

		it.idx++
	}

	it.cur = types.Candle{}
	it.ts = 0
	return false
}

func (it *chainedCandleIterator) Candle() types.Candle {
	return it.cur
}

func (it *chainedCandleIterator) Timestamp() types.Timestamp {
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

// type chainedIterator[T any] struct {
// 	iters  []Iterator[T]
// 	idx    int
// 	cur    T
// 	err    error
// 	closed bool
// }

// func NewChainedIterator[T any](iters ...Iterator[T]) Iterator[T] {
// 	return &chainedIterator[T]{
// 		iters: iters,
// 	}
// }

// func (it *chainedIterator[T]) Next() bool {
// 	if it.closed || it.err != nil {
// 		var zero T
// 		it.cur = zero
// 		return false
// 	}

// 	for it.idx < len(it.iters) {
// 		curIt := it.iters[it.idx]
// 		if curIt.Next() {
// 			it.cur = curIt.Item()
// 			return true
// 		}
// 		if err := curIt.Err(); err != nil {
// 			it.err = err
// 			var zero T
// 			it.cur = zero
// 			return false
// 		}
// 		if err := curIt.Close(); err != nil {
// 			it.err = err
// 			var zero T
// 			it.cur = zero
// 			return false
// 		}
// 		it.idx++
// 	}

// 	var zero T
// 	it.cur = zero
// 	return false
// }

// func (it *chainedIterator[T]) Item() T {
// 	return it.cur
// }

// func (it *chainedIterator[T]) Err() error {
// 	return it.err
// }

// func (it *chainedIterator[T]) Close() error {
// 	if it.closed {
// 		return nil
// 	}
// 	it.closed = true

// 	var firstErr error
// 	for _, sub := range it.iters {
// 		if sub == nil {
// 			continue
// 		}
// 		if err := sub.Close(); err != nil && firstErr == nil {
// 			firstErr = err
// 		}
// 	}
// 	return firstErr
// }

// type filteredIterator[T any] struct {
// 	base Iterator[T]
// 	keep func(T) bool

// 	cur T
// }

// func NewFilteredIterator[T any](base Iterator[T], keep func(T) bool) Iterator[T] {
// 	return &filteredIterator[T]{
// 		base: base,
// 		keep: keep,
// 	}
// }

// func (it *filteredIterator[T]) Next() bool {
// 	for it.base.Next() {
// 		item := it.base.Item()
// 		if it.keep(item) {
// 			it.cur = item
// 			return true
// 		}
// 	}
// 	var zero T
// 	it.cur = zero
// 	return false
// }

// func (it *filteredIterator[T]) Item() T {
// 	return it.cur
// }

// func (it *filteredIterator[T]) Err() error {
// 	return it.base.Err()
// }

// func (it *filteredIterator[T]) Close() error {
// 	return it.base.Close()
// }

// type bi5TickIterator struct {
// 	file     *os.File
// 	reader   io.Reader
// 	baseTime int64
// 	scale    int32

// 	cur    Tick
// 	err    error
// 	done   bool
// 	closed bool
// }

// func (it *bi5TickIterator) Next() bool {
// 	if it.closed || it.done || it.err != nil {
// 		var zero Tick
// 		it.cur = zero
// 		return false
// 	}

// 	tick, ok, err := readNextBI5Tick(it.reader, it.baseTime)
// 	if err != nil {
// 		it.err = err
// 		var zero Tick
// 		it.cur = zero
// 		return false
// 	}
// 	if !ok {
// 		it.done = true
// 		var zero Tick
// 		it.cur = zero
// 		return false
// 	}

// 	it.cur = tick
// 	return true
// }

// func (it *bi5TickIterator) Item() Tick {
// 	return it.cur
// }

// func (it *bi5TickIterator) Err() error {
// 	return it.err
// }

// func (it *bi5TickIterator) Close() error {
// 	if it.closed {
// 		return nil
// 	}
// 	it.closed = true
// 	if it.file != nil {
// 		return it.file.Close()
// 	}
// 	return nil
// }

// const bi5RecordSize = 20

// func readNextBI5Tick(r io.Reader, baseTimeMS int64) (Tick, bool, error) {
// 	var rec [bi5RecordSize]byte

// 	_, err := io.ReadFull(r, rec[:])
// 	if err != nil {
// 		if errors.Is(err, io.EOF) {
// 			return Tick{}, false, nil
// 		}
// 		if errors.Is(err, io.ErrUnexpectedEOF) {
// 			return Tick{}, false, io.ErrUnexpectedEOF
// 		}
// 		return Tick{}, false, err
// 	}

// 	msOffset := binary.BigEndian.Uint32(rec[0:4])
// 	askRaw := binary.BigEndian.Uint32(rec[4:8])
// 	bidRaw := binary.BigEndian.Uint32(rec[8:12])
// 	askVolBits := binary.BigEndian.Uint32(rec[12:16])
// 	bidVolBits := binary.BigEndian.Uint32(rec[16:20])

// 	t := Tick{
// 		Timemilli: types.Timemilli(baseTimeMS + int64(msOffset)),
// 		Bid:       types.Price(bidRaw),
// 		Ask:       types.Price(askRaw),
// 		AskVol:    math.Float32frombits(askVolBits),
// 		BidVol:    math.Float32frombits(bidVolBits),
// 	}

// 	return t, true, nil
// }

// func (s *Store) OpenTickIterator(key Key) (TickIterator, error) {
// 	if key.Kind != KindTick {
// 		return nil, fmt.Errorf("OpenTickIterator: key is not KindTick: %+v", key)
// 	}
// 	if key.TF != types.Ticks {
// 		return nil, fmt.Errorf("OpenTickIterator: key timeframe is not ticks: %+v", key)
// 	}

// 	path := s.PathForAsset(key)

// 	f, err := os.Open(path)
// 	if err != nil {
// 		return nil, fmt.Errorf("open tick file %s: %w", path, err)
// 	}

// 	lr, err := lzma.NewReader(f)
// 	if err != nil {
// 		_ = f.Close()
// 		return nil, fmt.Errorf("open lzma reader for %s: %w", path, err)
// 	}

// 	base := time.Date(
// 		key.Year,
// 		time.Month(key.Month),
// 		key.Day,
// 		key.Hour,
// 		0, 0, 0,
// 		time.UTC,
// 	)

// 	it := &bi5TickIterator{
// 		file:     f,
// 		reader:   bufio.NewReader(lr),
// 		baseTime: base.UnixMilli(),
// 		scale:    int32(types.PriceScale), // or your chosen price scale constant
// 	}

// 	return it, nil
// }

// type sliceIterator[T any] struct {
// 	items  []T
// 	idx    int
// 	cur    T
// 	closed bool
// }

// func NewSliceIterator[T any](items []T) Iterator[T] {
// 	return &sliceIterator[T]{
// 		items: items,
// 		idx:   0,
// 	}
// }

// func (it *sliceIterator[T]) Next() bool {
// 	if it.closed {
// 		var zero T
// 		it.cur = zero
// 		return false
// 	}
// 	if it.idx >= len(it.items) {
// 		var zero T
// 		it.cur = zero
// 		return false
// 	}
// 	it.cur = it.items[it.idx]
// 	it.idx++
// 	return true
// }

// func (it *sliceIterator[T]) Item() T {
// 	return it.cur
// }

// func (it *sliceIterator[T]) Err() error {
// 	return nil
// }

// func (it *sliceIterator[T]) Close() error {
// 	it.closed = true
// 	return nil
// }
