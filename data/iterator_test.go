package data

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// funcIterator via NewFuncIterator
// ---------------------------------------------------------------------------

func TestFuncIterator_HappyPath(t *testing.T) {
	t.Parallel()

	items := []int{1, 2, 3}
	idx := 0

	it := NewFuncIterator(
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
	it := NewFuncIterator(
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
	it := NewFuncIterator(
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
	it := NewFuncIterator(
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
	it := NewFuncIterator(
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
	it := NewFuncIterator(
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
	it := NewFuncIterator(
		func() (int, bool, error) { return 0, false, nil },
		func() error { return sentinel },
	)

	require.ErrorIs(t, it.Close(), sentinel)
}
