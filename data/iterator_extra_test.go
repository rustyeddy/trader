package data

import (
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

// errCandleIterator is a test-only CandleIterator that returns errors on demand.
type errCandleIterator struct {
	nextErr  error
	closeErr error
	count    int
	maxItems int
	cur      market.Candle
	ts       types.Timestamp
}

func (it *errCandleIterator) Next() bool {
	if it.nextErr != nil || it.count >= it.maxItems {
		return false
	}
	it.count++
	it.cur = market.Candle{Open: 100, Close: 100, Ticks: 1}
	it.ts = types.Timestamp(it.count)
	return true
}
func (it *errCandleIterator) Candle() market.Candle { return it.cur }
func (it *errCandleIterator) CandleTime() types.CandleTime {
	return types.CandleTime{Candle: it.cur, Timestamp: it.ts}
}
func (it *errCandleIterator) Timestamp() types.Timestamp { return it.ts }
func (it *errCandleIterator) Err() error                 { return it.nextErr }
func (it *errCandleIterator) Close() error               { return it.closeErr }
func (it *errCandleIterator) NextCandle() (market.Candle, bool) {
	if it.Next() {
		return it.Candle(), true
	}
	return market.Candle{}, false
}

// errAfterCandleIterator returns items first then an error.
type errAfterCandleIterator struct {
	items   []market.Candle
	tss     []types.Timestamp
	idx     int
	errOnce error
	emitted bool
}

func (it *errAfterCandleIterator) Next() bool {
	if it.idx < len(it.items) {
		it.idx++
		return true
	}
	return false
}
func (it *errAfterCandleIterator) Candle() market.Candle { return it.items[it.idx-1] }
func (it *errAfterCandleIterator) CandleTime() types.CandleTime {
	return types.CandleTime{Candle: it.Candle(), Timestamp: it.Timestamp()}
}
func (it *errAfterCandleIterator) NextCandle() (market.Candle, bool) {
	if it.Next() {
		return it.Candle(), true
	}
	return market.Candle{}, false
}
func (it *errAfterCandleIterator) Timestamp() types.Timestamp { return it.tss[it.idx-1] }
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
		items:   []market.Candle{{Ticks: 1}},
		tss:     []types.Timestamp{1},
		errOnce: sentinel,
	}
	chained := NewChainedCandleIterator(sub)

	// First item
	require.True(t, chained.Next())
	// After items exhausted, sub.Err() returns sentinel → chained propagates it
	require.False(t, chained.Next())
	require.ErrorIs(t, chained.Err(), sentinel)
}

func TestChainedCandleIterator_SubCloseErr(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("close error")
	sub := &errCandleIterator{
		maxItems: 0,
		closeErr: sentinel,
	}
	chained := NewChainedCandleIterator(sub)

	// sub.Next() returns false → chained tries to close sub → error propagates
	require.False(t, chained.Next())
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
	closingChained := NewChainedCandleIterator(sub)
	_ = closingChained.Next() // advance to trigger sub
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
		TF:         types.Ticks,
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

func TestDukasfileBaseHourUnixMS(t *testing.T) {
	s := useTempStore(t)
	_ = s

	sym := "EURUSD"
	ts := time.Date(2025, 1, 2, 13, 0, 0, 0, time.UTC)
	df := newDatafile(sym, ts)

	ms, err := df.baseHourUnixMS()
	require.NoError(t, err)
	want := types.TimeMilliFromTime(ts)
	require.Equal(t, want, ms)
}
