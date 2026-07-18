package datamanager

import (
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// funcIterator via NewFuncIterator
// ---------------------------------------------------------------------------

func TestFuncIterator_HappyPath(t *testing.T) {
	t.Parallel()

	items := []int{1, 2, 3}
	idx := 0

	it := newFuncIterator(
		func() (int, bool, error) {
			if idx >= len(items) {
				return 0, false, nil
			}
			v := items[idx]
			idx++
			return v, true, nil
		},
		nil,
	)

	var got []int
	for it.Next() {
		got = append(got, it.Item())
	}
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())
	require.Equal(t, items, got)
}

func TestFuncIterator_ErrorFromNextFn(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("next error")
	it := newFuncIterator(
		func() (int, bool, error) {
			return 0, false, sentinel
		},
		nil,
	)

	require.False(t, it.Next())
	require.Equal(t, 0, it.Item())
	require.ErrorIs(t, it.Err(), sentinel)
}

func TestFuncIterator_StopsAfterDone(t *testing.T) {
	t.Parallel()

	calls := 0
	it := newFuncIterator(
		func() (int, bool, error) {
			calls++
			return 0, false, nil
		},
		nil,
	)

	require.False(t, it.Next())
	// Should not call nextFn again once done
	require.False(t, it.Next())
	require.Equal(t, 1, calls)
}

func TestFuncIterator_StopsAfterError(t *testing.T) {
	t.Parallel()

	calls := 0
	sentinel := errors.New("test error")
	it := newFuncIterator(
		func() (int, bool, error) {
			calls++
			return 0, false, sentinel
		},
		nil,
	)

	require.False(t, it.Next())
	require.False(t, it.Next())
	require.Equal(t, 1, calls)
}

func TestFuncIterator_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	closeCalls := 0
	it := newFuncIterator(
		func() (int, bool, error) { return 0, false, nil },
		func() error {
			closeCalls++
			return nil
		},
	)

	require.NoError(t, it.Close())
	require.NoError(t, it.Close())
	require.Equal(t, 1, closeCalls)
}

func TestFuncIterator_StopsAfterClose(t *testing.T) {
	t.Parallel()

	calls := 0
	it := newFuncIterator(
		func() (int, bool, error) {
			calls++
			return 1, true, nil
		},
		nil,
	)

	require.NoError(t, it.Close())
	require.False(t, it.Next())
	require.Equal(t, 0, calls)
}

func TestFuncIterator_CloseError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("close failed")
	it := newFuncIterator(
		func() (int, bool, error) { return 0, false, nil },
		func() error { return sentinel },
	)

	require.ErrorIs(t, it.Close(), sentinel)
}

// ---------------------------------------------------------------------------
// candleSetIterator.CandleSet
// ---------------------------------------------------------------------------

func TestCandleSetIterator_CandleSet(t *testing.T) {
	t.Parallel()
	cs := HelperGenerateSyntheticCandles(t, "EUR_USD", 2024, 1, types.M1)
	it := cs.Iterator()
	require.Same(t, cs, it.CandleSet())
}

// errCandleIterator is a test-only CandleIterator that returns errors on demand.
type errCandleIterator struct {
	nextErr  error
	closeErr error
	count    int
	maxItems int
	cur      market.Candle
	ts       types.Timestamp
}

func (it *errCandleIterator) Next() (market.Candle, bool) {
	if it.nextErr != nil || it.count >= it.maxItems {
		return market.Candle{}, false
	}
	it.count++
	it.cur = market.Candle{Open: 100, Close: 100, Ticks: 1}
	it.ts = types.Timestamp(it.count)
	c := it.cur
	c.Timestamp = it.ts
	return c, true
}
func (it *errCandleIterator) Err() error   { return it.nextErr }
func (it *errCandleIterator) Close() error { return it.closeErr }

// errAfterCandleIterator returns items first then an error.
type errAfterCandleIterator struct {
	items   []market.Candle
	tss     []types.Timestamp
	idx     int
	errOnce error
	emitted bool
}

func (it *errAfterCandleIterator) Next() (market.Candle, bool) {
	if it.idx < len(it.items) {
		ct := it.items[it.idx]
		ct.Timestamp = it.tss[it.idx]
		it.idx++
		return ct, true
	}
	return market.Candle{}, false
}
func (it *errAfterCandleIterator) Err() error {
	if it.idx >= len(it.items) && !it.emitted {
		it.emitted = true
		return it.errOnce
	}
	return nil
}
func (it *errAfterCandleIterator) Close() error { return nil }

func TestChainedCandleIterator_SubErrAfterItems(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sub error")
	sub := &errAfterCandleIterator{
		items:   []market.Candle{{Ticks: 1}},
		tss:     []types.Timestamp{1},
		errOnce: sentinel,
	}
	chained := newChainedCandleIterator(sub)

	_, ok := chained.Next()
	require.True(t, ok)
	_, ok = chained.Next()
	require.False(t, ok)
	require.ErrorIs(t, chained.Err(), sentinel)
}

func TestChainedCandleIterator_NextExhausted(t *testing.T) {
	t.Parallel()

	sub := &errCandleIterator{maxItems: 1}
	chained := newChainedCandleIterator(sub)

	ct, ok := chained.Next()
	require.True(t, ok)
	require.Equal(t, market.Candle{Open: 100, Close: 100, Ticks: 1, Timestamp: 1}, ct)

	ct, ok = chained.Next()
	require.False(t, ok)
	require.Equal(t, market.Candle{}, ct)
}

func TestChainedCandleIterator_SubCloseErr(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("close error")
	sub := &errCandleIterator{
		maxItems: 0,
		closeErr: sentinel,
	}
	chained := newChainedCandleIterator(sub)

	_, ok := chained.Next()
	require.False(t, ok)
	require.ErrorIs(t, chained.Err(), sentinel)
}

func TestChainedCandleIterator_ClosePropagatesSubError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("close sub error")
	sub := &errCandleIterator{
		maxItems: 0,
		closeErr: sentinel,
	}
	closingChained := newChainedCandleIterator(sub)
	_, _ = closingChained.Next()
	err := closingChained.Close()
	_ = err
}

func TestOpenTickIterator_FileNotFound(t *testing.T) {
	s := useTempStore(t)

	k := Key{
		Kind:       KindTick,
		TF:         types.Ticks,
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Year:       2026,
		Month:      1,
		Day:        5,
		Hour:       10,
	}

	_, err := s.OpenTickIterator(k)
	require.Error(t, err)
	_ = s
}

func TestCloseCandleIterators_CloseError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("close error")
	sub := &errCandleIterator{closeErr: sentinel}

	err := closeCandleIterators([]market.CandleIterator{sub})
	require.ErrorIs(t, err, sentinel)
}

func TestCandleSetIterator_AlreadyClosed(t *testing.T) {
	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, types.H1)
	_ = s

	it := newCandleSetIterator(cs, types.TimeRange{})
	require.NoError(t, it.Close())
	_, ok := it.Next()
	require.False(t, ok)
	require.NoError(t, it.Close())
}

func TestCandleSetIterator_AfterDone(t *testing.T) {
	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, types.H1)
	_ = s

	it := newCandleSetIterator(cs, types.TimeRange{})
	for _, ok := it.Next(); ok; _, ok = it.Next() {
	}
	_, ok := it.Next()
	require.False(t, ok)
	require.NoError(t, it.Err())
}
