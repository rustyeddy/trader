package server

import (
	"errors"
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedEntropy(b byte) func([]byte) error {
	return func(p []byte) error {
		for i := range p {
			p[i] = b
		}
		return nil
	}
}

func TestEncodeULID_KnownValues(t *testing.T) {
	assert.Equal(t, "00000000000000000000000000", encodeULID(0, [10]byte{}))
	assert.Equal(t, "7ZZZZZZZZZZZZZZZZZZZZZZZZZ", encodeULID(1<<48-1, [10]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255}))
	// The ULID spec's example timestamp, 2016-07-30T23:54:10.385Z.
	assert.Equal(t, "01ARYZ6S41", encodeULID(1469918176385, [10]byte{})[:10])
	// One low entropy bit is the last digit.
	assert.Equal(t, "00000000000000000000000001", encodeULID(0, [10]byte{9: 1}))
	// The first entropy bit is the top bit of the 11th digit (G = 16).
	assert.Equal(t, "0000000000G000000000000000", encodeULID(0, [10]byte{0: 0x80}))
}

func TestRunIDGenerator_IsAULIDAndSortsByTime(t *testing.T) {
	now := time.UnixMilli(1700000000000)
	g := &RunIDGenerator{Now: func() time.Time { return now }, Entropy: fixedEntropy(7)}
	a, err := g.NewRunID()
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`), a)
	now = now.Add(time.Millisecond)
	b, err := g.NewRunID()
	require.NoError(t, err)
	assert.Less(t, a, b, "a later millisecond sorts later")
	assert.Equal(t, RunIDLen, len(a))
}

func TestRunIDGenerator_IsMonotonicWithinAMillisecond(t *testing.T) {
	now := time.UnixMilli(1700000000000)
	g := &RunIDGenerator{Now: func() time.Time { return now }, Entropy: fixedEntropy(0)}
	var ids []string
	for range 5 {
		id, err := g.NewRunID()
		require.NoError(t, err)
		ids = append(ids, id)
	}
	assert.True(t, sort.StringsAreSorted(ids), "same-millisecond IDs sort in issue order: %v", ids)
	assert.Len(t, map[string]bool{ids[0]: true, ids[1]: true, ids[2]: true, ids[3]: true, ids[4]: true}, 5)
}

func TestRunIDGenerator_AClockThatStepsBackNeitherRepeatsNorReorders(t *testing.T) {
	now := time.UnixMilli(1700000005000)
	g := &RunIDGenerator{Now: func() time.Time { return now }, Entropy: fixedEntropy(1)}
	a, _ := g.NewRunID()
	now = time.UnixMilli(1700000000000)
	b, err := g.NewRunID()
	require.NoError(t, err)
	assert.Greater(t, b, a)
}

func TestRunIDGenerator_Errors(t *testing.T) {
	t.Run("entropy failure", func(t *testing.T) {
		g := &RunIDGenerator{Entropy: func([]byte) error { return errors.New("no entropy") }}
		_, err := g.NewRunID()
		assert.ErrorContains(t, err, "no entropy")
	})
	t.Run("exhausted within one millisecond", func(t *testing.T) {
		now := time.UnixMilli(1700000000000)
		g := &RunIDGenerator{Now: func() time.Time { return now }, Entropy: fixedEntropy(0xFF)}
		_, err := g.NewRunID() // entropy is already the maximum
		require.NoError(t, err)
		_, err = g.NewRunID()
		assert.ErrorIs(t, err, ErrRunIDExhausted)
	})
	t.Run("time out of range", func(t *testing.T) {
		g := &RunIDGenerator{Now: func() time.Time { return time.UnixMilli(-1) }}
		_, err := g.NewRunID()
		assert.Error(t, err)
		g = &RunIDGenerator{Now: func() time.Time { return time.UnixMilli(1 << 48) }}
		_, err = g.NewRunID()
		assert.Error(t, err)
	})
}

func TestRandomRunID_IsUniqueAndSafeConcurrently(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 250 {
				id, err := RandomRunID()
				if !assert.NoError(t, err) {
					return
				}
				mu.Lock()
				seen[id] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Len(t, seen, 2000)
}
