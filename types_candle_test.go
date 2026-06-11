package trader

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadCandleSet is an internal helper for trader type processing.
func loadCandleSet(t *testing.T) *candleSet {
	t.Helper()
	fname := "./testdata/DAT_ASCII_EURUSD_M1_2025.csv"
	if _, err := os.Stat(fname); err != nil {
		t.Skip("candle test dataset missing")
	}
	set := &candleSet{
		Filepath: fname,
	}
	return set
}

// TestIterator verifies expected behavior for this component.
func TestIterator(t *testing.T) {
	cs := loadCandleSet(t)

	expected := Candle{Open: 1035030, High: 1035140, Low: 1035030, Close: 1035140}
	it := cs.Iterator()
	it.Next()
	assert.Equal(t, Timestamp(1735768800), it.StartTime())

	ca := it.Candle()
	assert.Equal(t, expected, ca)

	i := 0
	for it.Next() {
		i++
	}

	assert.Equal(t, i, 372023)
}

// TestReadCandleSetFile verifies expected behavior for this component.
func TestReadCandleSetFile(t *testing.T) {
	cs := loadCandleSet(t)
	s := cs.Stats()
	assert.Equal(t, 524158, s.TotalBars)
	assert.Equal(t, 372024, s.PresentBars)
	assert.Equal(t, 152134, s.MissingBars)
	assert.Equal(t, 965, s.GapCount)
	assert.Equal(t, 52, s.WeekendGaps)
	assert.Equal(t, 15, s.SuspiciousGaps)
}

// TestAggregateH1 verifies expected behavior for this component.
func TestAggregateH1(t *testing.T) {
	cs := loadCandleSet(t)
	h1, err := cs.AggregateH1(50)
	require.NoError(t, err)
	h1.BuildGapReport()
	s := h1.Stats()

	assert.Equal(t, 8736, s.TotalBars)
	assert.Equal(t, 6212, s.PresentBars)
	assert.Equal(t, 2524, s.MissingBars)
	assert.Equal(t, 54, s.GapCount)
	assert.Equal(t, 52, s.WeekendGaps)
	assert.Equal(t, 2, s.SuspiciousGaps)
	assert.Equal(t, 49, s.LongestGapBars)

	// fmt.Printf("H1: %+v\n", h1)
}
