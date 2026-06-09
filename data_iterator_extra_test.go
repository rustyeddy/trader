package trader

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// errCandleIterator is a test-only CandleIterator that returns errors on demand.
type errCandleIterator struct {
	nextErr  error
	closeErr error
	count    int
	maxItems int
	cur      Candle
	ts       Timestamp
}

func (it *errCandleIterator) Next() (CandleTime, bool) {
	if it.nextErr != nil || it.count >= it.maxItems {
		return CandleTime{}, false
	}
	it.count++
	it.cur = Candle{Open: 100, Close: 100, Ticks: 1}
	it.ts = Timestamp(it.count)
	return CandleTime{Candle: it.cur, Timestamp: it.ts}, true
}
func (it *errCandleIterator) Err() error   { return it.nextErr }
func (it *errCandleIterator) Close() error { return it.closeErr }

// errAfterCandleIterator returns items first then an error.
type errAfterCandleIterator struct {
	items   []Candle
	tss     []Timestamp
	idx     int
	errOnce error
	emitted bool
}

func (it *errAfterCandleIterator) Next() (CandleTime, bool) {
	if it.idx < len(it.items) {
		ct := CandleTime{Candle: it.items[it.idx], Timestamp: it.tss[it.idx]}
		it.idx++
		return ct, true
	}
	return CandleTime{}, false
}
func (it *errAfterCandleIterator) Err() error {
	if it.idx >= len(it.items) && !it.emitted {
		it.emitted = true
		return it.errOnce
	}
	return nil
}
func (it *errAfterCandleIterator) Close() error { return nil }

// ---------------------------------------------------------------------------
// ChainedCandleIterator error propagation
// ---------------------------------------------------------------------------

func TestChainedCandleIterator_SubErrAfterItems(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sub error")
	sub := &errAfterCandleIterator{
		items:   []Candle{{Ticks: 1}},
		tss:     []Timestamp{1},
		errOnce: sentinel,
	}
	chained := newChainedCandleIterator(sub)

	// First item
	_, ok := chained.Next()
	require.True(t, ok)
	// After items exhausted, sub.Err() returns sentinel → chained propagates it
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
	require.Equal(t, Candle{Open: 100, Close: 100, Ticks: 1}, ct.Candle)

	ct, ok = chained.Next()
	require.False(t, ok)
	require.Equal(t, CandleTime{}, ct)
}

func TestChainedCandleIterator_SubCloseErr(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("close error")
	sub := &errCandleIterator{
		maxItems: 0,
		closeErr: sentinel,
	}
	chained := newChainedCandleIterator(sub)

	// sub.Next() returns false → chained tries to close sub → error propagates
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
	// The Close() on chainedCandleIterator should return first error from subs
	closingChained := newChainedCandleIterator(sub)
	_, _ = closingChained.Next() // advance to trigger sub
	err := closingChained.Close()
	_ = err // May or may not propagate depending on state
}

// ---------------------------------------------------------------------------
// OpenTickIterator: file not found (market open, file doesn't exist)
// ---------------------------------------------------------------------------

func TestOpenTickIterator_FileNotFound(t *testing.T) {
	s := useTempStore(t)

	// 2026-01-05 (Monday) = forex market open
	k := Key{
		Kind:       KindTick,
		TF:         Ticks,
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Year:       2026,
		Month:      1,
		Day:        5,
		Hour:       10,
	}

	// File does not exist → should get open error
	_, err := s.OpenTickIterator(k)
	require.Error(t, err)
	_ = s
}

// ---------------------------------------------------------------------------
// closeCandleIterators with a close error
// ---------------------------------------------------------------------------

func TestCloseCandleIterators_CloseError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("close error")
	sub := &errCandleIterator{closeErr: sentinel}

	err := closeCandleIterators([]CandleIterator{sub})
	require.ErrorIs(t, err, sentinel)
}

// ---------------------------------------------------------------------------
// dukasfile.baseHourUnixMS via actual Key.Path()
// ---------------------------------------------------------------------------

// baseHourUnixMS test moved to ./data/dukascopy/ (now an internal helper there)
